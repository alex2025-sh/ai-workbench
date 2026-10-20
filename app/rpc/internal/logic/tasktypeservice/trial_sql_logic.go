package tasktypeservicelogic

import (
	"context"

	"encoding/json"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/identity"
	"github.com/alex2025-sh/ai-workbench/app/rpc/internal/svc"
	"github.com/alex2025-sh/ai-workbench/app/rpc/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zeromicro/go-zero/core/logx"
)

type TrialSqlLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewTrialSqlLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TrialSqlLogic {
	return &TrialSqlLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

// 试跑 SQL：在只读事务中执行，返回列与前 20 行；配置页据此生成字段
func (l *TrialSqlLogic) TrialSql(in *pb.TrialSqlReq) (*pb.TrialSqlResp, error) {
	if _, err := identity.Require(l.ctx); err != nil {
		return nil, err
	}
	res, err := l.svcCtx.Datasources.Query(l.ctx, in.Datasource, in.Query, 20)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error()) // SQL 错误原样返回给配置人员
	}
	rows := make([]map[string]any, 0, len(res.Rows))
	for _, r := range res.Rows {
		m := map[string]any{"key": r.Key}
		for k, v := range r.Values {
			m[k] = v
		}
		rows = append(rows, m)
	}
	raw, _ := json.Marshal(rows)
	return &pb.TrialSqlResp{Columns: res.Columns, RowsJson: raw, HasMore: res.Truncated}, nil
}
