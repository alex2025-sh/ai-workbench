// Command wbctl 是任务类型配置的命令行工具：在没有前端时用 YAML 编写、校验任务类型，
// 并导出提交给 SaveTaskTypeDraft 的 JSON。
//
//	wbctl validate FILE...   校验（YAML 或 JSON），有 error 时退出码为 1
//	wbctl json FILE          输出 body_json
//	wbctl plan FILE          输出编译后的执行计划
//	wbctl node-types         输出节点类型目录（含配置 schema），即 CatalogService.ListNodeTypes 的内容
//	wbctl schema             输出 task_type.schema.json
//
// 以下命令连接数据库（-dsn，或环境变量 WB_DSN），用于本地开发：
//
//	wbctl publish FILE...                         直接发布为新版本（跳过变更集与审批，仅限开发环境）
//	wbctl run RUN_ID                              查看 run 的节点状态与事件
//	wbctl signal -run ID -node N -option O        模拟人工节点上的一次点击
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	apischema "github.com/alex2025-sh/ai-workbench/api/schema"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
)

func main() {
	logx.SetLevel(logx.ErrorLevel)
	logx.DisableStat()
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validate(os.Args[2:])
	case "json":
		err = withBody(os.Args[2:], func(body []byte) error { return writeJSON(json.RawMessage(body)) })
	case "plan":
		err = withBody(os.Args[2:], func(body []byte) error {
			res := spec.Check(body)
			if !res.OK() {
				printIssues(os.Args[2], res.Issues)
				return fmt.Errorf("compile failed")
			}
			return writeJSON(res.Plan)
		})
	case "node-types":
		err = nodeTypes()
	case "schema":
		_, err = os.Stdout.Write(apischema.TaskTypeSchema())
	case "publish":
		err = publish(os.Args[2:])
	case "run":
		err = showRun(os.Args[2:])
	case "signal":
		err = signal(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "wbctl:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wbctl validate FILE... | json FILE | plan FILE | node-types | schema | publish FILE... | run RUN_ID | signal -run ID -node N -option O")
	os.Exit(2)
}

func readBody(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return spec.FromYAML(raw)
	default:
		return raw, nil
	}
}

func withBody(args []string, fn func([]byte) error) error {
	if len(args) != 1 {
		usage()
	}
	body, err := readBody(args[0])
	if err != nil {
		return err
	}
	return fn(body)
}

func validate(files []string) error {
	if len(files) == 0 {
		usage()
	}
	failed := 0
	for _, f := range files {
		body, err := readBody(f)
		if err != nil {
			return err
		}
		res := spec.Check(body)
		printIssues(f, res.Issues)
		if res.OK() {
			fmt.Printf("%s: ok (%d nodes, %d warnings)\n", f, len(res.Plan.Order), len(res.Issues))
		} else {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files have errors", failed, len(files))
	}
	return nil
}

func printIssues(file string, issues []compiler.Issue) {
	for _, i := range issues {
		fmt.Printf("%s: %s %s [%s] %s\n", file, i.Severity, i.Path, i.Code, i.Message)
	}
}

type nodeTypeOut struct {
	Type         string          `json:"type"`
	Category     string          `json:"category"`
	DisplayName  string          `json:"display_name"`
	Description  string          `json:"description"`
	PortRule     string          `json:"port_rule"`
	FixedPorts   []string        `json:"fixed_ports,omitempty"`
	ConfigSchema json.RawMessage `json:"config_schema"`
}

func nodeTypes() error {
	var out []nodeTypeOut
	for _, t := range dsl.NodeTypes() {
		s, err := apischema.NodeConfigSchema(string(t.Type))
		if err != nil {
			return err
		}
		out = append(out, nodeTypeOut{
			Type: string(t.Type), Category: string(t.Category), DisplayName: t.DisplayName, Description: t.Description,
			PortRule: string(t.PortRule), FixedPorts: t.FixedPorts, ConfigSchema: s,
		})
	}
	return writeJSON(out)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func dbFlags(name string, args []string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	dsn := fs.String("dsn", os.Getenv("WB_DSN"), "platform database DSN (parseTime=true&loc=UTC)")
	return fs, dsn
}

func services(dsn string) (*bootstrap.Services, error) {
	if dsn == "" {
		return nil, fmt.Errorf("-dsn or WB_DSN is required")
	}
	return bootstrap.Build(bootstrap.Config{DataSource: dsn, Owner: "wbctl"}, nil)
}

func publish(args []string) error {
	fs, dsn := dbFlags("publish", args)
	by := fs.String("by", "wbctl", "published_by")
	_ = fs.Parse(args)
	svc, err := services(*dsn)
	if err != nil {
		return err
	}
	for _, f := range fs.Args() {
		body, err := readBody(f)
		if err != nil {
			return err
		}
		res, err := registry.PublishDirect(context.Background(), svc.Conn, body, *by, engine.SystemClock{}.Now())
		if err != nil {
			return err
		}
		printIssues(f, res.Issues)
		if res.Version == 0 {
			return fmt.Errorf("%s: not published", f)
		}
		fmt.Printf("%s: published %s@%d\n", f, res.Key, res.Version)
	}
	return nil
}

func showRun(args []string) error {
	fs, dsn := dbFlags("run", args)
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	id, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		return err
	}
	svc, err := services(*dsn)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := svc.Engine.GetRun(ctx, id)
	if err != nil {
		return err
	}
	events, err := svc.Store.Events(ctx, id)
	if err != nil {
		return err
	}
	type ev struct {
		At      string          `json:"at"`
		Node    string          `json:"node,omitempty"`
		Type    string          `json:"type"`
		Actor   string          `json:"actor"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	var evs []ev
	for _, e := range events {
		x := ev{At: e.CreatedAt.Format("2006-01-02 15:04:05.000"), Node: e.NodeId, Type: e.Type, Actor: e.Actor}
		if e.Payload.Valid {
			x.Payload = json.RawMessage(e.Payload.String)
		}
		evs = append(evs, x)
	}
	return writeJSON(map[string]any{"run": v.Run, "nodes": v.Nodes, "events": evs})
}

func signal(args []string) error {
	fs, dsn := dbFlags("signal", args)
	run := fs.Int64("run", 0, "run id")
	node := fs.String("node", "", "human_choice node id")
	option := fs.String("option", "", "option key")
	iteration := fs.Int("iteration", 0, "iteration")
	actor := fs.String("actor", "wbctl", "who clicked")
	_ = fs.Parse(args)
	svc, err := services(*dsn)
	if err != nil {
		return err
	}
	res, err := svc.Engine.Signal(context.Background(), engine.SignalCmd{RunID: *run, NodeID: *node, Iteration: *iteration, Option: *option, Actor: *actor})
	if err != nil {
		return err
	}
	return writeJSON(res)
}
