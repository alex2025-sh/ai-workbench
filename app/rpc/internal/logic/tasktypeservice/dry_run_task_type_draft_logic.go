package tasktypeservicelogic

import (
	"context"
	"errors"
	"time"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/dryrun"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/spec"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DryRunTaskTypeDraftLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewDryRunTaskTypeDraftLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DryRunTaskTypeDraftLogic {
	return &DryRunTaskTypeDraftLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 试运行：动作节点只记录不执行，虚拟时钟
func (l *DryRunTaskTypeDraftLogic) DryRunTaskTypeDraft(in *pb.DryRunTaskTypeDraftReq) (*pb.DryRunResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	if l.svcCtx.DryRun == nil {
		return nil, status.Error(codes.FailedPrecondition, "dry run is not configured (Workbench.DryRunDataSource)")
	}
	if len(in.SampleJson) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one sample is required")
	}
	// 有草稿时试运行草稿（编译后用版本号 0 的临时定义），否则试运行最新发布版本。
	var def *engine.Definition
	out := &pb.DryRunResp{Passed: true}
	d, err := l.svcCtx.TaskTypes.Find(l.ctx, in.Key)
	switch {
	case err == nil:
		out.Revision, out.ContentHash = d.Revision, d.ContentHash
		res := spec.Check(d.Body)
		if !res.OK() {
			out.Passed, out.Issues = false, toIssues(res.Issues)
			return out, nil
		}
		if def, err = engine.NewDefinition(in.Key, 0, res.Spec, res.Plan); err != nil {
			return nil, err
		}
	case errors.Is(err, registry.ErrNoDraft):
		row, err := findPlan(l.ctx, l.svcCtx, in.Key, 0)
		if err != nil {
			return nil, err
		}
		if def, err = l.svcCtx.Plans.Load(l.ctx, row.PlanId, int(row.Version)); err != nil {
			return nil, err
		}
		out.ContentHash = row.Checksum
	default:
		return nil, draftError(err)
	}
	in2 := dryrun.Input{Def: def, Choices: in.Choices, Now: time.Now()}
	for _, s := range in.SampleJson {
		in2.Samples = append(in2.Samples, s)
	}
	report, err := l.svcCtx.DryRun.Run(l.ctx, in2)
	if err != nil {
		return nil, err
	}
	for _, c := range report.Cases {
		pc := &pb.DryRunCase{SampleIndex: int32(c.Index), AssigneeId: c.Assignee, EndStatus: c.EndStatus}
		for _, st := range c.Path {
			pc.Path = append(pc.Path, &pb.DryRunStep{NodeId: st.NodeID, Iteration: int32(st.Iteration), Port: st.Port, Status: st.Status})
		}
		for _, sd := range c.Sends {
			pc.Sends = append(pc.Sends, &pb.DryRunSend{NodeId: sd.NodeID, Kind: sd.Kind, Target: sd.Target, Summary: sd.Summary, RenderedJson: sd.Rendered})
		}
		for _, tp := range c.Timeline {
			pc.Timeline = append(pc.Timeline, &pb.DryRunTimePoint{NodeId: tp.NodeID, Kind: tp.Kind, OffsetMs: tp.Offset.Milliseconds()})
		}
		for _, is := range c.Issues {
			pc.Issues = append(pc.Issues, &pb.Issue{Code: is.Code, Severity: "error", NodeId: is.NodeID, Message: is.Message})
		}
		// 停在等待中的人工节点（没有预设选择）是正常结果；样本无效、节点失败、超出上限都算不通过。
		if len(c.Issues) > 0 || (c.EndStatus != "succeeded" && c.EndStatus != "waiting") {
			out.Passed = false
		}
		out.Cases = append(out.Cases, pc)
	}
	return out, nil
}
