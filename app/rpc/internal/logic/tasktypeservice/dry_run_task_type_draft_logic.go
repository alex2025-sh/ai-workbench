package tasktypeservicelogic

import (
	"context"
	"time"

	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"github.com/alex2025-sh/ai-workbench/internal/dryrun"

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
	// 草稿尚未实现：对最新发布版本试运行。接入草稿后改为编译草稿内容，并返回草稿的 revision 与 content_hash。
	row, err := findPlan(l.ctx, l.svcCtx, in.Key, 0)
	if err != nil {
		return nil, err
	}
	def, err := l.svcCtx.Plans.Load(l.ctx, row.PlanId, int(row.Version))
	if err != nil {
		return nil, err
	}
	in2 := dryrun.Input{Def: def, Choices: in.Choices, Now: time.Now()}
	for _, s := range in.SampleJson {
		in2.Samples = append(in2.Samples, s)
	}
	report, err := l.svcCtx.DryRun.Run(l.ctx, in2)
	if err != nil {
		return nil, err
	}
	out := &pb.DryRunResp{ContentHash: row.Checksum, Passed: true}
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
