package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/model"
)

// 运行干预：run 进入 needs_intervention 后由运维重试节点、跳过节点（指定端口）或取消 run（见 Cancel）。
// 与其他推进一样经 update 在 run 锁内修改状态，所有干预写入 wf_event，带操作人与原因。
//
// 暂停期间 run 的其他 job 不会等待：执行 job 被判为过期撤销，定时到期时节点已不算“等待中”而作废，
// 卡片 effect 也被撤销。恢复时（resume）按节点状态把这些 job 找回，而不是只把 run 改回 running。

// 运行干预的错误。
var (
	ErrNotPaused      = errors.New("engine: run is not waiting for intervention")
	ErrRunEnded       = errors.New("engine: run has ended")
	ErrUnknownNode    = errors.New("engine: unknown node")
	ErrNothingToRetry = errors.New("engine: node has nothing to retry (not failed, no failed job)")
	ErrNodeConcluded  = errors.New("engine: node is not in progress (already concluded or not reached)")
	ErrInvalidPort    = errors.New("engine: invalid port for node")
)

// RetryNode 重试 needs_intervention 的 run 中的一个节点：失败的节点重新执行（执行次数清零，重试策略重新生效），
// 失败的 job（卡片发送失败、处理出错被隔离）重新放回队列；然后恢复整个 run。
func (e *Engine) RetryNode(ctx context.Context, runID int64, nodeID, actor, reason string) error {
	_, jobs, err := update(ctx, e, runID, 0, func(s *runState) (struct{}, error) {
		if s.run.Status != RunNeedsIntervention {
			return struct{}{}, ErrNotPaused
		}
		r := s.nodes[nodeID]
		if r == nil {
			return struct{}{}, ErrUnknownNode
		}
		all, err := s.t.jobs.FindByRun(s.t.ctx, uint64(s.run.ID))
		if err != nil {
			return struct{}{}, err
		}
		hasFailedJob := slices.ContainsFunc(all, func(j *model.WfJob) bool {
			return j.NodeId == nodeID && j.Iteration == r.Iteration && j.Status == string(JobFailed)
		})
		if NodeStatus(r.Status) != NodeFailed && !hasFailedJob {
			return struct{}{}, ErrNothingToRetry
		}
		if NodeStatus(r.Status) == NodeFailed {
			s.setNode(nodeID, NodeReady, func(r *model.WfNodeRun) {
				r.Attempt = 0
				r.Output = sql.NullString{}
				r.FinishedAt = sql.NullTime{}
			})
		}
		s.event(nodeID, "node_retried", actor, map[string]any{"reason": reason})
		s.resume(all, "", actor, reason)
		return struct{}{}, nil
	})
	if err == nil {
		e.drive(ctx, jobs)
	}
	return err
}

// SkipNode 让一个进行中（就绪、执行中、等待中或失败）的节点以 port 完成并继续传播，撤销它尚未执行的 job。
// run 在运行中（如人工节点长时间无人处理）或 needs_intervention 时都可以跳过；后者同时恢复 run。
func (e *Engine) SkipNode(ctx context.Context, runID int64, nodeID, port, actor, reason string) error {
	_, jobs, err := update(ctx, e, runID, 0, func(s *runState) (struct{}, error) {
		if s.run.Status != RunRunning && s.run.Status != RunNeedsIntervention {
			return struct{}{}, ErrRunEnded
		}
		r := s.nodes[nodeID]
		if r == nil {
			return struct{}{}, ErrUnknownNode
		}
		switch NodeStatus(r.Status) {
		case NodeReady, NodeRunning, NodeWaiting, NodeFailed:
		default:
			return struct{}{}, ErrNodeConcluded
		}
		n, _ := s.def.node(nodeID)
		ports, err := dsl.Ports(n)
		if err != nil {
			return struct{}{}, err
		}
		if !slices.Contains(ports, port) {
			return struct{}{}, fmt.Errorf("%w: %q (ports: %v)", ErrInvalidPort, port, ports)
		}
		if s.run.Status == RunNeedsIntervention {
			all, err := s.t.jobs.FindByRun(s.t.ctx, uint64(s.run.ID))
			if err != nil {
				return struct{}{}, err
			}
			s.resume(all, nodeID, actor, reason)
		}
		s.cancelJobs(nodeID, JobExecute, JobTimer, JobTimeout, JobWake, JobEffect) // 执行中的结果提交时会因节点已完成而丢弃
		s.event(nodeID, "node_skipped_manually", actor, map[string]any{"port": port, "reason": reason})
		out, _ := json.Marshal(map[string]any{"skipped": true, "actor": actor, "reason": reason})
		s.complete(nodeID, port, out)
		return struct{}{}, nil
	})
	if err == nil {
		e.drive(ctx, jobs)
	}
	return err
}

// resume 把 needs_intervention 的 run 恢复为 running，并补回暂停期间被消耗的 job（except 节点除外）：
//   - 就绪或执行中的节点：确保有待执行的 execute job。执行中的那次（还持有认领的 job）被撤销，
//     它的结果提交时会因认领失效而丢弃，节点退回就绪重新执行；
//   - 等待中的节点：被作废的定时按原到期时间补回，被撤销或失败的卡片 effect 立即补回。
//
// 补回的是新 job：幂等键带上被替代的 job id（:r<id>），重试次数与认领凭证从头开始；旧 job 保持原状。
func (s *runState) resume(all []*model.WfJob, except, actor, reason string) {
	byNode := map[string][]*model.WfJob{}
	for _, j := range all {
		if r := s.nodes[j.NodeId]; r != nil && j.Iteration == r.Iteration {
			byNode[j.NodeId] = append(byNode[j.NodeId], j)
		}
	}
	for _, id := range s.def.Plan.Order {
		r := s.nodes[id]
		if id == except || r == nil {
			continue
		}
		jobs := byNode[id]
		switch NodeStatus(r.Status) {
		case NodeReady, NodeRunning:
			last := latest(jobs, JobExecute)
			if last != nil && last.Status == string(JobPending) && last.Owner == "" {
				continue // 还没被认领的 execute job（如退避等待中）照常执行
			}
			if NodeStatus(r.Status) == NodeRunning {
				s.setNode(id, NodeReady, nil)
			}
			s.cancelJobs(id, JobExecute)
			s.addJob(id, JobExecute, s.now, nil, replaces(last))
		case NodeWaiting:
			for _, j := range jobs {
				kind := JobKind(j.Kind)
				switch {
				case j.Status == string(JobPending) || superseded(jobs, j):
				case kind == JobTimer || kind == JobTimeout:
					// 节点仍在等待而定时已结束：到期时 run 处于暂停，定时作废了。
					s.addJob(id, kind, j.DueAt, nil, replaces(j))
				case kind == JobWake && j.LastError == voidedWhilePaused:
					// 提醒正常到期后也是 done（节点仍在等待），只补回暂停期间作废的。
					s.addJob(id, kind, j.DueAt, json.RawMessage(j.Payload.String), replaces(j))
				case kind == JobEffect && j.Status != string(JobDone):
					s.addJob(id, kind, s.now, json.RawMessage(j.Payload.String), replaces(j))
				}
			}
		}
	}
	s.setRunStatus(RunRunning)
	s.event("", "run_resumed", actor, map[string]any{"reason": reason})
}

// replaces 返回补回 job 的幂等键后缀；没有被替代的 job 时为空。
func replaces(j *model.WfJob) string {
	if j == nil {
		return ""
	}
	return fmt.Sprintf(":r%d", j.Id)
}

// superseded 报告 j 是否已被之前的一次恢复补回过（存在幂等键以 :r<j.id> 结尾的 job）。
func superseded(jobs []*model.WfJob, j *model.WfJob) bool {
	suffix := replaces(j)
	return slices.ContainsFunc(jobs, func(x *model.WfJob) bool { return strings.HasSuffix(x.IdemKey, suffix) })
}

func hasPending(jobs []*model.WfJob, kind JobKind) bool {
	return slices.ContainsFunc(jobs, func(j *model.WfJob) bool { return JobKind(j.Kind) == kind && j.Status == string(JobPending) })
}

// latest 返回某类 job 中 id 最大的一个。
func latest(jobs []*model.WfJob, kind JobKind) *model.WfJob {
	var out *model.WfJob
	for _, j := range jobs {
		if JobKind(j.Kind) == kind && (out == nil || j.Id > out.Id) {
			out = j
		}
	}
	return out
}
