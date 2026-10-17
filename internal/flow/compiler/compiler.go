// Package compiler 在保存、校验与发布时检查 workflow，并生成不可变的执行计划（Plan）。
//
// 复杂的结构判断全部放在编译期：环检测、端口校验、变量引用可达性、
// rework 边的支配关系与单入口判定、view / assignment 的继承解析。
// 运行时只读 Plan，不再遍历原始 DSL。
package compiler

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/pkg/expr"
)

// Plan 是编译产物，发布后不可变，运行实例启动时锁定版本。
// 遍历一律使用切片（Order、In、Out、Loops），map 只用于按键查找。
type Plan struct {
	Graph dsl.Graph `json:"graph"`
	Entry string    `json:"entry"`

	// Order 是去掉 rework 边后的拓扑序。
	Order []string `json:"order"`

	// In 是每个节点的普通入边（汇合语义只看普通边）；Out 是全部出边（含 rework）。切片顺序与 DSL 中的边顺序一致。
	In  map[string][]dsl.Edge `json:"in"`
	Out map[string][]dsl.Edge `json:"out"`

	// Ports 是每个节点的输出端口。
	Ports map[string][]string `json:"ports"`

	// Loops 是每条 rework 边对应的环区域。
	Loops []LoopRegion `json:"loops,omitempty"`

	// Bindings 是 human_choice 与 lark_card 节点在编译期解析后的卡片模板与分配策略（已处理继承）。
	Bindings map[string]ResolvedBinding `json:"bindings,omitempty"`
}

// LoopRegion 是由一条 rework 边围成的单入口区域。
type LoopRegion struct {
	EdgeID        string   `json:"edge_id"`
	Head          string   `json:"head"`  // 回边目标，必须支配回边源
	Tail          string   `json:"tail"`  // 回边源（human_choice）
	Nodes         []string `json:"nodes"` // 区域内节点，按拓扑序
	MaxIterations int      `json:"max_iterations"`
	OnExhausted   string   `json:"on_exhausted"`
}

// 继承来源。
const (
	FromSelf     = "self"
	FromTaskType = "task_type"
)

// ResolvedBinding 是节点最终使用的配置，以及它来自哪里：self、上游人工节点 ID 或 task_type。
type ResolvedBinding struct {
	View           *dsl.ViewBinding       `json:"view"`
	ViewFrom       string                 `json:"view_from"`
	Assignment     *dsl.AssignmentBinding `json:"assignment"`
	AssignmentFrom string                 `json:"assignment_from"`
}

// Severity 是问题级别：error 阻止提交与发布，warning 只提示。
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// IssueCode 是问题分类，供画布高亮与 AI 校验—修复循环使用。
type IssueCode string

const (
	IssueSchema            IssueCode = "schema"           // 不符合 task_type.schema.json
	IssueDuplicateID       IssueCode = "duplicate_id"     // 节点、边、来源、端口重名
	IssueTrigger           IssueCode = "trigger"          // 必须且只能有一个 trigger
	IssueBadConfig         IssueCode = "bad_config"       // 节点配置无法解析
	IssueBadEdge           IssueCode = "bad_edge"         // 连线两端不存在、指向 trigger、重复连线
	IssueUnknownPort       IssueCode = "unknown_port"     // 源端口不是该节点的输出端口
	IssueUnconnectedPort   IssueCode = "unconnected_port" // 端口没有连线（error 端口除外）
	IssueCycle             IssueCode = "cycle"            // 普通连线成环
	IssueUnreachable       IssueCode = "unreachable"      // 从 trigger 不可达
	IssueBadExpression     IssueCode = "bad_expression"   // 表达式或模板语法错误、读取当前时间
	IssueBadReference      IssueCode = "bad_reference"    // 引用了不存在或不在上游的节点、未知命名空间、未定义的模板
	IssueReworkSource      IssueCode = "rework_source"    // rework 边的源节点不是 human_choice
	IssueReworkNotDominant IssueCode = "rework_not_dominant"
	IssueReworkEntry       IssueCode = "rework_entry"   // 环区域有多个入口
	IssueReworkOverlap     IssueCode = "rework_overlap" // 环区域相交但不嵌套
	IssueReworkLimits      IssueCode = "rework_limits"  // 次数上限或 on_exhausted 配置有误
	IssueInheritConflict   IssueCode = "inherit_conflict"
	IssueMissingBinding    IssueCode = "missing_binding" // 继承链上没有可用的 view / assignment
	IssueUnused            IssueCode = "unused"          // 定义了但没有被使用（warning）
)

// Issue 是一条问题。Path 用 ID 定位，如 workflow.nodes.ask_rm.config.timeout、workflow.edges.e3。
type Issue struct {
	Code     IssueCode `json:"code"`
	Severity Severity  `json:"severity"`
	Path     string    `json:"path,omitempty"`
	NodeID   string    `json:"node_id,omitempty"`
	EdgeID   string    `json:"edge_id,omitempty"`
	Message  string    `json:"message"`
}

// HasErrors 报告是否存在 error 级别的问题。
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Defaults 是编译时需要的任务类型默认值。
type Defaults struct {
	View       *dsl.ViewBinding
	Assignment *dsl.AssignmentBinding
}

// Compile 校验 workflow 并生成 Plan。存在 error 级别的问题时返回的 Plan 为 nil。
func Compile(g dsl.Graph, d Defaults) (*Plan, []Issue) {
	c := &compilation{g: g, defaults: d}
	c.run()
	if HasErrors(c.issues) {
		return nil, c.issues
	}
	return c.plan(), c.issues
}

type compilation struct {
	g        dsl.Graph
	defaults Defaults
	issues   []Issue

	index   map[string]int // 节点 ID → 在 g.Nodes 中的下标
	configs map[string]any // 节点 ID → 解码后的配置
	ports   map[string][]string
	edges   []dsl.Edge // 两端存在且端口合法的边，保持 DSL 顺序
	// virtual 是 rework 边的源节点到 on_exhausted 节点的隐含连线：次数用尽时引擎直接走这条路。
	// 它参与拓扑排序、可达性、上游判断与继承，但不出现在 Plan 的 In / Out 中。
	virtual []dsl.Edge
	entry   string

	order    []string
	pos      map[string]int // 节点 ID → 拓扑序位置
	preds    map[string][]string
	succs    map[string][]string
	loops    []LoopRegion
	bindings map[string]ResolvedBinding
}

func nodePath(id string) string { return "workflow.nodes." + id }
func edgePath(id string) string { return "workflow.edges." + id }

func (c *compilation) errorf(code IssueCode, path, node, edge, format string, args ...any) {
	c.issues = append(c.issues, Issue{Code: code, Severity: SeverityError, Path: path, NodeID: node, EdgeID: edge, Message: fmt.Sprintf(format, args...)})
}

func (c *compilation) warnf(code IssueCode, path, node, edge, format string, args ...any) {
	c.issues = append(c.issues, Issue{Code: code, Severity: SeverityWarning, Path: path, NodeID: node, EdgeID: edge, Message: fmt.Sprintf(format, args...)})
}

func (c *compilation) run() {
	c.checkNodes()
	c.checkEdges()
	if c.entry == "" {
		return
	}
	if !c.topoSort() {
		return
	}
	c.checkReachability()
	c.checkPortsConnected()
	c.checkRework()
	c.checkExpressions()
	c.resolveBindings()
}

// ---- 节点 ----

func (c *compilation) checkNodes() {
	c.index = map[string]int{}
	c.configs = map[string]any{}
	c.ports = map[string][]string{}
	triggers := 0
	for i, n := range c.g.Nodes {
		p := nodePath(n.ID)
		if _, dup := c.index[n.ID]; dup {
			c.errorf(IssueDuplicateID, p, n.ID, "", "节点 ID %q 重复", n.ID)
			continue
		}
		c.index[n.ID] = i
		if _, ok := dsl.LookupNodeType(n.Type); !ok {
			c.errorf(IssueBadConfig, p+".type", n.ID, "", "未知的节点类型 %q", n.Type)
			continue
		}
		if n.Type == dsl.NodeTrigger {
			triggers++
			if c.entry == "" {
				c.entry = n.ID
			}
		}
		cfg, err := dsl.DecodeConfig(n.Type, n.Config)
		if err != nil {
			c.errorf(IssueBadConfig, p+".config", n.ID, "", "节点配置无法解析：%v", err)
			continue
		}
		c.configs[n.ID] = cfg
		ports, err := dsl.Ports(n)
		if err != nil {
			c.errorf(IssueBadConfig, p+".config", n.ID, "", "无法确定输出端口：%v", err)
			continue
		}
		c.checkConfig(n, cfg)
		seen := map[string]bool{}
		for _, port := range ports {
			if seen[port] {
				c.errorf(IssueDuplicateID, p+".config", n.ID, "", "端口 %q 重复", port)
			}
			seen[port] = true
		}
		c.ports[n.ID] = ports
		for j, se := range n.SideEffects {
			sp := fmt.Sprintf("%s.side_effects.%d", p, j)
			info, ok := dsl.LookupNodeType(se.Action)
			if !ok || info.Category != dsl.CategoryAction {
				c.errorf(IssueBadConfig, sp+".action", n.ID, "", "旁路动作只能是动作节点类型，得到 %q", se.Action)
				continue
			}
			if _, err := dsl.DecodeConfig(se.Action, se.Config); err != nil {
				c.errorf(IssueBadConfig, sp+".config", n.ID, "", "旁路动作配置无法解析：%v", err)
			}
		}
	}
	switch {
	case triggers == 0:
		c.errorf(IssueTrigger, "workflow.nodes", "", "", "workflow 需要一个 trigger 节点作为入口")
	case triggers > 1:
		c.errorf(IssueTrigger, "workflow.nodes", "", "", "workflow 只能有一个 trigger 节点，当前有 %d 个", triggers)
	}
}

// checkConfig 做 schema 之外、只依赖单个节点的语义检查。
func (c *compilation) checkConfig(n dsl.Node, cfg any) {
	p := nodePath(n.ID) + ".config"
	switch v := cfg.(type) {
	case *dsl.HumanChoiceConfig:
		fields := map[string]bool{}
		if v.Form != nil {
			for _, f := range v.Form.Fields {
				fields[f.Key] = true
			}
		}
		ports := 0
		for i, o := range v.Options {
			if o.Snooze == nil {
				ports++
			}
			for _, rf := range o.RequiredFields {
				if !fields[rf] {
					c.errorf(IssueBadReference, fmt.Sprintf("%s.options.%d.required_fields", p, i), n.ID, "", "选项 %q 要求的字段 %q 不在 form.fields 中", o.Key, rf)
				}
			}
		}
		if ports == 0 {
			c.errorf(IssueBadConfig, p+".options", n.ID, "", "至少需要一个不是推迟（snooze）的选项")
		}
	case *dsl.SendEmailConfig:
		if v.Body != "" && v.View != "" {
			c.errorf(IssueBadConfig, p, n.ID, "", "body 与 view 只能二选一")
		}
	}
}

// ---- 连线 ----

func (c *compilation) checkEdges() {
	ids := map[string]bool{}
	triples := map[string]bool{}
	for _, e := range c.g.Edges {
		p := edgePath(e.ID)
		if ids[e.ID] {
			c.errorf(IssueDuplicateID, p, "", e.ID, "连线 ID %q 重复", e.ID)
			continue
		}
		ids[e.ID] = true
		fromIdx, okFrom := c.index[e.From]
		_, okTo := c.index[e.To]
		if !okFrom {
			c.errorf(IssueBadEdge, p+".from", "", e.ID, "源节点 %q 不存在", e.From)
		}
		if !okTo {
			c.errorf(IssueBadEdge, p+".to", "", e.ID, "目标节点 %q 不存在", e.To)
		}
		if !okFrom || !okTo {
			continue
		}
		if c.g.Nodes[c.index[e.To]].Type == dsl.NodeTrigger {
			c.errorf(IssueBadEdge, p+".to", "", e.ID, "不能连到 trigger 节点")
			continue
		}
		if e.Kind != "" && e.Kind != dsl.EdgeNormal && e.Kind != dsl.EdgeRework {
			c.errorf(IssueBadEdge, p+".kind", "", e.ID, "未知的连线类型 %q", e.Kind)
			continue
		}
		ports, known := c.ports[e.From]
		if !known {
			continue // 源节点配置有误，已报告
		}
		if !contains(ports, e.Port) {
			c.errorf(IssueUnknownPort, p+".port", e.From, e.ID, "节点 %q（%s）没有端口 %q，可用端口：%s",
				e.From, c.g.Nodes[fromIdx].Type, e.Port, strings.Join(ports, ", "))
			continue
		}
		key := e.From + "\x00" + e.Port + "\x00" + e.To
		if triples[key] {
			c.errorf(IssueBadEdge, p, "", e.ID, "与另一条连线重复：%s.%s → %s", e.From, e.Port, e.To)
			continue
		}
		triples[key] = true
		if !e.IsRework() && e.From == e.To {
			c.errorf(IssueCycle, p, e.From, e.ID, "普通连线不能连回自身；退回请使用 rework 连线")
			continue
		}
		c.edges = append(c.edges, e)
		if e.IsRework() {
			if _, ok := c.index[e.OnExhausted]; ok {
				c.virtual = append(c.virtual, dsl.Edge{ID: e.ID + "#exhausted", From: e.From, To: e.OnExhausted})
			}
		}
	}
}

// graphEdges 返回参与图算法的边：普通边 + 隐含的 on_exhausted 连线。
func (c *compilation) graphEdges() []dsl.Edge {
	var out []dsl.Edge
	for _, e := range c.edges {
		if !e.IsRework() {
			out = append(out, e)
		}
	}
	return append(out, c.virtual...)
}

// ---- 图结构 ----

// topoSort 先对普通边排序；再校验隐含的 on_exhausted 连线不会指回上游，加入后重新排序。
func (c *compilation) topoSort() bool {
	var normal []dsl.Edge
	for _, e := range c.edges {
		if !e.IsRework() {
			normal = append(normal, e)
		}
	}
	if !c.sortEdges(normal) {
		return false
	}
	var keep []dsl.Edge
	for _, v := range c.virtual {
		if c.walk(v.From, c.preds)[v.To] {
			id := strings.TrimSuffix(v.ID, "#exhausted")
			c.errorf(IssueReworkLimits, edgePath(id)+".on_exhausted", "", id, "on_exhausted 指向的节点 %q 不能在退回源 %q 的上游或环区域内", v.To, v.From)
			continue
		}
		keep = append(keep, v)
	}
	c.virtual = keep
	return c.sortEdges(c.graphEdges())
}

// sortEdges 做 Kahn 排序，入度为 0 的节点按 DSL 中的顺序出队，保证结果稳定。
func (c *compilation) sortEdges(edges []dsl.Edge) bool {
	c.preds = map[string][]string{}
	c.succs = map[string][]string{}
	c.order = nil
	indeg := map[string]int{}
	for _, e := range edges {
		c.succs[e.From] = append(c.succs[e.From], e.To)
		c.preds[e.To] = append(c.preds[e.To], e.From)
		indeg[e.To]++
	}
	var ready []string
	for _, n := range c.g.Nodes {
		if _, ok := c.configs[n.ID]; ok && indeg[n.ID] == 0 {
			ready = append(ready, n.ID)
		}
	}
	c.pos = map[string]int{}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		c.pos[id] = len(c.order)
		c.order = append(c.order, id)
		var next []string
		for _, s := range c.succs[id] {
			indeg[s]--
			if indeg[s] == 0 {
				next = append(next, s)
			}
		}
		ready = c.mergeByIndex(ready, next)
	}
	if len(c.order) < len(c.configs) {
		var stuck []string
		for _, n := range c.g.Nodes {
			if _, ok := c.configs[n.ID]; ok {
				if _, done := c.pos[n.ID]; !done {
					stuck = append(stuck, n.ID)
				}
			}
		}
		c.errorf(IssueCycle, "workflow.edges", "", "", "普通连线构成了环，涉及节点：%s；退回请使用 rework 连线", strings.Join(stuck, ", "))
		return false
	}
	return true
}

func (c *compilation) mergeByIndex(a, b []string) []string {
	out := append(append([]string(nil), a...), b...)
	sort.SliceStable(out, func(i, j int) bool { return c.index[out[i]] < c.index[out[j]] })
	return out
}

func (c *compilation) checkReachability() {
	seen := map[string]bool{c.entry: true}
	queue := []string{c.entry}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, e := range append(append([]dsl.Edge(nil), c.edges...), c.virtual...) {
			if e.From == id && !seen[e.To] {
				seen[e.To] = true
				queue = append(queue, e.To)
			}
		}
	}
	for _, id := range c.order {
		if !seen[id] {
			c.errorf(IssueUnreachable, nodePath(id), id, "", "节点 %q 从 trigger 不可达", id)
		}
	}
}

func (c *compilation) checkPortsConnected() {
	used := map[string]bool{}
	for _, e := range c.edges {
		used[e.From+"\x00"+e.Port] = true
	}
	for _, id := range c.order {
		for _, port := range c.ports[id] {
			if port == dsl.PortError {
				continue // 未连线时失败进入人工干预
			}
			if !used[id+"\x00"+port] {
				c.errorf(IssueUnconnectedPort, nodePath(id), id, "", "节点 %q 的端口 %q 没有连线", id, port)
			}
		}
		if n := c.node(id); n.Type == dsl.NodeMerge && len(c.preds[id]) < 2 {
			c.warnf(IssueUnused, nodePath(id), id, "", "汇合节点 %q 只有 %d 条入边", id, len(c.preds[id]))
		}
	}
}

func (c *compilation) node(id string) dsl.Node { return c.g.Nodes[c.index[id]] }

// ---- 受控的环 ----

func (c *compilation) checkRework() {
	var doms map[string]map[string]bool
	for _, e := range c.edges {
		if !e.IsRework() {
			continue
		}
		p := edgePath(e.ID)
		if c.node(e.From).Type != dsl.NodeHumanChoice {
			c.errorf(IssueReworkSource, p+".from", e.From, e.ID, "rework 连线的源节点必须是 human_choice")
			continue
		}
		if e.MaxIterations < 1 {
			c.errorf(IssueReworkLimits, p+".max_iterations", "", e.ID, "rework 连线必须设置 max_iterations（≥ 1）")
		}
		if _, ok := c.index[e.OnExhausted]; !ok {
			c.errorf(IssueReworkLimits, p+".on_exhausted", "", e.ID, "on_exhausted 指向的节点 %q 不存在", e.OnExhausted)
		}
		if doms == nil {
			doms = c.dominators()
		}
		if !doms[e.From][e.To] {
			c.errorf(IssueReworkNotDominant, p, e.From, e.ID, "退回目标 %q 必须位于 %q 的每条上游路径上", e.To, e.From)
			continue
		}
		region := c.region(e.To, e.From)
		inRegion := map[string]bool{}
		for _, id := range region {
			inRegion[id] = true
		}
		// 目标支配源时区域必然只有一个入口；这里是防御性检查。on_exhausted 不在区域内已在拓扑排序时检查。
		for _, id := range region {
			if id == e.To {
				continue
			}
			for _, pr := range c.preds[id] {
				if !inRegion[pr] {
					c.errorf(IssueReworkEntry, nodePath(id), id, e.ID, "环区域 %s → %s 只能从 %q 进入，但 %q 有来自区域外 %q 的连线", e.To, e.From, e.To, id, pr)
				}
			}
		}
		c.loops = append(c.loops, LoopRegion{EdgeID: e.ID, Head: e.To, Tail: e.From, Nodes: region, MaxIterations: e.MaxIterations, OnExhausted: e.OnExhausted})
	}
	for i := range c.loops {
		for j := i + 1; j < len(c.loops); j++ {
			a, b := c.loops[i], c.loops[j]
			inter := intersect(a.Nodes, b.Nodes)
			if inter > 0 && inter != len(a.Nodes) && inter != len(b.Nodes) {
				c.errorf(IssueReworkOverlap, edgePath(b.EdgeID), "", b.EdgeID, "环区域与连线 %q 的环区域相交但不嵌套", a.EdgeID)
			}
		}
	}
}

// dominators 在普通边构成的 DAG 上按拓扑序计算支配集：dom(n) = {n} ∪ ⋂ dom(pred)。
func (c *compilation) dominators() map[string]map[string]bool {
	dom := map[string]map[string]bool{}
	for _, id := range c.order {
		var acc map[string]bool
		for _, p := range c.preds[id] {
			if acc == nil {
				acc = map[string]bool{}
				for k := range dom[p] {
					acc[k] = true
				}
				continue
			}
			for k := range acc {
				if !dom[p][k] {
					delete(acc, k)
				}
			}
		}
		if acc == nil {
			acc = map[string]bool{}
		}
		acc[id] = true
		dom[id] = acc
	}
	return dom
}

// region 返回从 head 到 tail 的所有路径上的节点（head 的后代 ∩ tail 的祖先），按拓扑序。
func (c *compilation) region(head, tail string) []string {
	desc := c.walk(head, c.succs)
	anc := c.walk(tail, c.preds)
	var out []string
	for _, id := range c.order {
		if desc[id] && anc[id] {
			out = append(out, id)
		}
	}
	return out
}

func (c *compilation) walk(start string, next map[string][]string) map[string]bool {
	seen := map[string]bool{start: true}
	stack := []string{start}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, n := range next[id] {
			if !seen[n] {
				seen[n] = true
				stack = append(stack, n)
			}
		}
	}
	return seen
}

func (c *compilation) inLoop(id string) bool {
	for _, l := range c.loops {
		if contains(l.Nodes, id) {
			return true
		}
	}
	return false
}

// ---- 表达式与变量引用 ----

func (c *compilation) checkExpressions() {
	ancestors := map[string]map[string]bool{}
	for _, id := range c.order {
		anc := c.walk(id, c.preds)
		delete(anc, id)
		ancestors[id] = anc
	}
	for _, id := range c.order {
		n := c.node(id)
		scope := Scope{Roots: []string{expr.RootPayload, expr.RootTask, expr.RootNodes}}
		if c.inLoop(id) {
			scope.Roots = append(scope.Roots, expr.RootLoop)
		}
		scope.Nodes = func(ref string) string {
			if _, ok := c.index[ref]; !ok {
				return fmt.Sprintf("节点 %q 不存在", ref)
			}
			if !ancestors[id][ref] {
				return fmt.Sprintf("节点 %q 不在 %q 的上游，读取不到它的输出", ref, id)
			}
			return ""
		}
		for _, is := range CheckValue(n.Config, nodePath(id)+".config", scope) {
			is.NodeID = id
			c.issues = append(c.issues, is)
		}
		for j, se := range n.SideEffects {
			for _, is := range CheckValue(se.Config, fmt.Sprintf("%s.side_effects.%d.config", nodePath(id), j), scope) {
				is.NodeID = id
				c.issues = append(c.issues, is)
			}
		}
	}
}

// ---- view / assignment 继承 ----

type inherited struct {
	view       *dsl.ViewBinding
	viewFrom   string
	assign     *dsl.AssignmentBinding
	assignFrom string
}

// resolveBindings 沿拓扑序计算每个节点能继承到的配置：只有 human_choice 会把自己的配置传给下游，
// human_choice 与 lark_card 在未显式配置时使用继承值；多个上游给出不同配置时报错。
func (c *compilation) resolveBindings() {
	out := map[string][]inherited{} // 节点 → 传给下游的候选（去重后）
	c.bindings = map[string]ResolvedBinding{}
	for _, id := range c.order {
		var in []inherited
		if id == c.entry {
			in = []inherited{{view: c.defaults.View, viewFrom: FromTaskType, assign: c.defaults.Assignment, assignFrom: FromTaskType}}
		}
		for _, p := range c.preds[id] {
			in = append(in, out[p]...)
		}
		n := c.node(id)
		var view *dsl.ViewBinding
		var assign *dsl.AssignmentBinding
		switch cfg := c.configs[id].(type) {
		case *dsl.HumanChoiceConfig:
			view, assign = cfg.View, cfg.Assignment
		case *dsl.LarkCardConfig:
			view, assign = cfg.View, cfg.To
		default:
			out[id] = dedupInherited(in)
			continue
		}
		field := "assignment"
		if n.Type == dsl.NodeLarkCard {
			field = "to"
		}
		rb := ResolvedBinding{View: view, ViewFrom: FromSelf, Assignment: assign, AssignmentFrom: FromSelf}
		if view == nil {
			from, ok := c.pick(id, "view", in, func(h inherited) (string, string) { return dsl.Canonical(h.view), h.viewFrom })
			rb.ViewFrom = from
			for _, h := range in {
				if ok && h.viewFrom == from {
					rb.View = h.view
					break
				}
			}
			if ok && rb.View == nil {
				c.errorf(IssueMissingBinding, nodePath(id)+".config.view", id, "", "节点 %q 没有可用的卡片模板：请配置 view，或在任务类型上设置默认 view", id)
			}
		}
		if assign == nil {
			from, ok := c.pick(id, field, in, func(h inherited) (string, string) { return dsl.Canonical(h.assign), h.assignFrom })
			rb.AssignmentFrom = from
			for _, h := range in {
				if ok && h.assignFrom == from {
					rb.Assignment = h.assign
					break
				}
			}
			if ok && rb.Assignment == nil {
				c.errorf(IssueMissingBinding, nodePath(id)+".config."+field, id, "", "节点 %q 没有可用的分配策略：请配置 %s，或在任务类型上设置默认 assignment", id, field)
			}
		}
		c.bindings[id] = rb
		if n.Type == dsl.NodeHumanChoice {
			out[id] = []inherited{{view: rb.View, viewFrom: id, assign: rb.Assignment, assignFrom: id}}
		} else {
			out[id] = dedupInherited(in)
		}
	}
}

// pick 在上游候选中选出唯一的配置来源。候选内容不一致时报 inherit_conflict 并返回 ok=false；
// 内容一致但来自多个上游时取字典序最小的来源，保证结果稳定。
func (c *compilation) pick(id, field string, in []inherited, key func(inherited) (string, string)) (from string, ok bool) {
	values := map[string][]string{}
	var order []string
	for _, h := range in {
		k, f := key(h)
		if _, seen := values[k]; !seen {
			order = append(order, k)
		}
		values[k] = append(values[k], f)
	}
	if len(order) == 0 {
		return "", true
	}
	if len(order) > 1 {
		var froms []string
		for _, k := range order {
			froms = append(froms, values[k][0])
		}
		c.errorf(IssueInheritConflict, nodePath(id)+".config."+field, id, "", "节点 %q 未配置 %s，不同上游路径继承到的配置不一致（来自 %s），请显式配置", id, field, strings.Join(froms, "、"))
		return "", false
	}
	froms := values[order[0]]
	sort.Strings(froms)
	return froms[0], true
}

func dedupInherited(in []inherited) []inherited {
	seen := map[string]bool{}
	var out []inherited
	for _, h := range in {
		k := h.viewFrom + "\x00" + h.assignFrom
		if !seen[k] {
			seen[k] = true
			out = append(out, h)
		}
	}
	return out
}

// ---- 产出 ----

func (c *compilation) plan() *Plan {
	p := &Plan{
		Graph:    c.g,
		Entry:    c.entry,
		Order:    c.order,
		In:       map[string][]dsl.Edge{},
		Out:      map[string][]dsl.Edge{},
		Ports:    c.ports,
		Loops:    c.loops,
		Bindings: c.bindings,
	}
	for _, e := range c.edges {
		p.Out[e.From] = append(p.Out[e.From], e)
		if !e.IsRework() {
			p.In[e.To] = append(p.In[e.To], e)
		}
	}
	return p
}

// MarshalPlan 序列化 Plan，用于写入 wf_plan。
func MarshalPlan(p *Plan) ([]byte, error) { return json.Marshal(p) }

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func intersect(a, b []string) int {
	n := 0
	for _, x := range a {
		if contains(b, x) {
			n++
		}
	}
	return n
}
