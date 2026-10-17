// Package appeal 是风控工单（appeal 表）的平台内置 skill，供 VIP 工单提醒使用。
//
// appeal 表属于业务系统，这里只读访问（应连接从库）。本地开发使用 deploy/mock/appeal.sql 中的 mock 表。
// 线上接入后，这两个 skill 应沉淀到 Skillhub，DSL 不需要改动。
package appeal

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
)

// Skill 名称。
const (
	ScanVIP   = "appeal.scan_vip"
	GetStatus = "appeal.get_status"
)

// Register 把两个 skill 注册到动作运行时的内置 skill 表。
func Register(s *action.Skills, conn sqlx.SqlConn) {
	if s.Builtin == nil {
		s.Builtin = map[string]action.SkillFunc{}
	}
	s.Builtin[ScanVIP] = func(ctx context.Context, in json.RawMessage) (any, error) { return scan(ctx, conn, in) }
	s.Builtin[GetStatus] = func(ctx context.Context, in json.RawMessage) (any, error) { return status(ctx, conn, in) }
}

// Record 是扫描出的一条工单，字段名与表的列名一致，供来源的 mapping 用 record.<列名> 引用。
type Record struct {
	ID            int64  `db:"id" json:"id"`
	AppealType    string `db:"appeal_type" json:"appeal_type"`
	AppealID      string `db:"appeal_id" json:"appeal_id"`
	AppealStatus  string `db:"appeal_status" json:"appeal_status"`
	UID           int64  `db:"uid" json:"uid"`
	BizCode       string `db:"biz_code" json:"biz_code"`
	OrderAmount   string `db:"order_amount" json:"order_amount"`
	CaseGroup     string `db:"case_group" json:"case_group"`
	Reviewer      string `db:"reviewer" json:"reviewer"`
	CommitTime    int64  `db:"commit_time" json:"commit_time"`
	VIPLabelType  string `db:"vip_label_type" json:"vip_label_type"`
	VIPLevel      int64  `db:"vip_level" json:"vip_level"`
	UpdatedAtUnix int64  `db:"updated_ts" json:"updated_ts"`
}

type scanInput struct {
	BatchSize  int    `json:"batch_size"`
	LagSeconds *int   `json:"lag_seconds"` // 安全延迟，默认 60
	Cursor     string `json:"cursor"`
}

// DefaultLagSeconds 是扫描的默认安全延迟。
const DefaultLagSeconds = 60

// ScanOutput 是增量扫描的结果。
type ScanOutput struct {
	Items      []Record `json:"items"`
	NextCursor string   `json:"next_cursor"`
}

// scan 按 (updated_at, id) 增量扫描，游标格式为 "<unix 秒>:<id>"。
//
// 只扫描 lag_seconds 之前的行（updated_at < NOW() - lag）：updated_at 取的是语句执行时间，
// 长事务里的行提交得晚，从库也有复制延迟，游标若紧贴当前时间，这些行可见时游标已经越过它们，会被永久漏掉。
// 延迟窗口覆盖这类情况；超过窗口的延迟仍可能漏，接入 binlog 或 Kafka 后可彻底解决。
func scan(ctx context.Context, conn sqlx.SqlConn, raw json.RawMessage) (any, error) {
	var in scanInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("%s: input: %w", ScanVIP, err)
	}
	if in.BatchSize <= 0 || in.BatchSize > 1000 {
		in.BatchSize = 200
	}
	lag := DefaultLagSeconds
	if in.LagSeconds != nil && *in.LagSeconds >= 0 {
		lag = *in.LagSeconds
	}
	ts, id, err := parseCursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	const q = "select `id`, `appeal_type`, `appeal_id`, `appeal_status`, `uid`, `biz_code`, `order_amount`, `case_group`, `reviewer`, " +
		"`commit_time`, `vip_label_type`, `vip_level`, unix_timestamp(`updated_at`) as `updated_ts` from `appeal` " +
		"where (`updated_at` > from_unixtime(?) or (`updated_at` = from_unixtime(?) and `id` > ?)) and `updated_at` < now() - interval ? second " +
		"order by `updated_at`, `id` limit ?"
	var rows []Record
	if err := conn.QueryRowsCtx(ctx, &rows, q, ts, ts, id, lag, in.BatchSize); err != nil {
		return nil, fmt.Errorf("%s: %w", ScanVIP, err)
	}
	out := ScanOutput{Items: rows, NextCursor: in.Cursor}
	if n := len(rows); n > 0 {
		out.NextCursor = fmt.Sprintf("%d:%d", rows[n-1].UpdatedAtUnix, rows[n-1].ID)
	}
	if out.Items == nil {
		out.Items = []Record{}
	}
	return out, nil
}

func parseCursor(c string) (int64, int64, error) {
	if c == "" {
		return 0, 0, nil
	}
	parts := strings.SplitN(c, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("%s: bad cursor %q", ScanVIP, c)
	}
	ts, err1 := strconv.ParseInt(parts[0], 10, 64)
	id, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("%s: bad cursor %q", ScanVIP, c)
	}
	return ts, id, nil
}

type statusInput struct {
	AppealID string `json:"appeal_id"`
}

// Status 是工单的当前状态。
type Status struct {
	Found        bool   `json:"found"`
	AppealStatus string `json:"appeal_status"`
	Reviewer     string `json:"reviewer"`
}

func status(ctx context.Context, conn sqlx.SqlConn, raw json.RawMessage) (any, error) {
	var in statusInput
	if err := json.Unmarshal(raw, &in); err != nil || in.AppealID == "" {
		return nil, fmt.Errorf("%s: appeal_id is required", GetStatus)
	}
	var row struct {
		AppealStatus string `db:"appeal_status"`
		Reviewer     string `db:"reviewer"`
	}
	err := conn.QueryRowCtx(ctx, &row, "select `appeal_status`, `reviewer` from `appeal` where `appeal_id` = ? limit 1", in.AppealID)
	switch err {
	case nil:
		return Status{Found: true, AppealStatus: row.AppealStatus, Reviewer: row.Reviewer}, nil
	case sqlx.ErrNotFound:
		return Status{}, nil
	default:
		return nil, fmt.Errorf("%s: %w", GetStatus, err)
	}
}
