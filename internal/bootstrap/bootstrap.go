// Package bootstrap 把引擎、任务中心、动作实现与 ticker 组装在一起，供 app/worker 与端到端测试共用。
package bootstrap

import (
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/flow/engine"
	"github.com/alex2025-sh/ai-workbench/internal/flow/nodes"
	"github.com/alex2025-sh/ai-workbench/internal/ingest"
	"github.com/alex2025-sh/ai-workbench/internal/lark"
	"github.com/alex2025-sh/ai-workbench/internal/skill/appeal"
	"github.com/alex2025-sh/ai-workbench/internal/task"
	"github.com/alex2025-sh/ai-workbench/internal/ticker"
)

// Config 是组装所需的配置。
type Config struct {
	DataSource    string        // 平台库（wf_*、task*、cfg_*），需要 parseTime=true&loc=UTC
	BizDataSource string        `json:",optional"` // 业务库只读连接，供内置 skill 使用；为空时不注册 appeal skill
	RelationsFile string        `json:",optional"` // 本地关系表（如 deploy/mock/vip_rm.yaml）
	Owner         string        `json:",optional"` // 进程标识，默认 worker-<pid>
	Lease         time.Duration `json:",default=2m"`
}

// Services 是组装后的组件。
type Services struct {
	Conn    sqlx.SqlConn
	Store   *engine.Store
	Plans   *engine.Plans
	Engine  *engine.Engine
	Tasks   *task.Store
	Actions *action.Runtime
	Lark    *lark.Mock
	Cron    *ingest.CronRunner
	Ticker  *ticker.Ticker
}

// Build 组装全部组件。clock 为 nil 时使用系统时钟。
func Build(c Config, clock engine.Clock) (*Services, error) {
	if clock == nil {
		clock = engine.SystemClock{}
	}
	sqlx.DisableStmtLog() // 只保留慢查询与错误日志
	conn := sqlx.NewMysql(c.DataSource)
	s := &Services{Conn: conn, Store: engine.NewStore(conn), Plans: engine.NewPlans(conn), Tasks: task.NewStore(conn), Lark: lark.NewMock()}

	var relations assign.RelationSource = &assign.StaticRelations{}
	if c.RelationsFile != "" {
		r, err := assign.LoadRelations(c.RelationsFile)
		if err != nil {
			return nil, err
		}
		relations = r
	}
	skills := &action.Skills{}
	if c.BizDataSource != "" {
		appeal.Register(skills, sqlx.NewMysql(c.BizDataSource))
	}
	s.Actions = action.NewRuntime()
	for _, a := range []action.Action{
		skills,
		assign.Local{Relations: relations},
		s.Lark,
		task.SyncAction{Tasks: s.Tasks},
		action.Logger{K: action.KindSendEmail},
		action.Logger{K: action.KindEmitEvent},
		action.Logger{K: action.KindLLM, Reply: []byte(`{"text":"(mock) LLM 尚未接入"}`)},
	} {
		if err := s.Actions.Register(a); err != nil {
			return nil, err
		}
	}
	owner := c.Owner
	if owner == "" {
		owner = fmt.Sprintf("worker-%d", time.Now().UnixNano()%1_000_000)
	}
	s.Engine = engine.New(s.Store, s.Plans, nodes.Default(), s.Actions, clock, engine.Options{Owner: owner, Lease: c.Lease})
	s.Cron = ingest.NewCronRunner(conn, s.Plans, s.Tasks, s.Engine, s.Actions, clock)
	s.Ticker = &ticker.Ticker{Cron: s.Cron}
	return s, nil
}
