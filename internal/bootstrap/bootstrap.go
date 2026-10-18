// Package bootstrap 把引擎、任务中心、动作实现与 ticker 组装在一起，供 app/worker 与端到端测试共用。
package bootstrap

import (
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/action"
	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/directory"
	"github.com/alex2025-sh/ai-workbench/internal/dryrun"
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
	DataSource    string // 平台库（wf_*、task*、cfg_*），需要 parseTime=true&loc=UTC
	BizDataSource string `json:",optional"` // 业务库只读连接，供内置 skill 使用；为空时不注册 appeal skill
	// DryRunDataSource 是专用的试运行库（结构同 deploy/sql），为空时不提供试运行。
	// 每次试运行都会清空其中的运行数据，不能与 DataSource 相同；worker 与 ticker 不连接它。
	DryRunDataSource string `json:",optional"`
	RelationsFile    string `json:",optional"`  // 本地关系表（如 deploy/mock/vip_rm.yaml）
	GroupsFile       string `json:",optional"`  // 本地用户组（如 deploy/mock/groups.yaml），供组任务的决策鉴权与可认领列表使用
	PermissionsFile  string `json:",optional"`  // 本地接口级权限（如 deploy/mock/permissions.yaml）；为空时没有任何管理权限
	Owner            string `json:",optional"`  // 进程标识，默认 worker-<随机数>，只用于排查
	Parallelism      int    `json:",default=4"` // 一次推进（含一次 tick 的每一批）同时执行的 job 数
}

// ReadCommitted 在 DSN 上加上 READ-COMMITTED 隔离级别（已指定时不改）。
// 引擎的正确性只依赖 run 行锁，不需要可重复读的间隙锁；去掉间隙锁可以避免不同 run 的事务互相死锁。
// TiDB 本身没有间隙锁，同样适用。
func ReadCommitted(dsn string) string {
	if strings.Contains(dsn, "transaction_isolation") || strings.Contains(dsn, "tx_isolation") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "transaction_isolation=%27READ-COMMITTED%27"
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
	Groups  directory.Groups
	Authz   directory.Authorizer
	DryRun  *dryrun.Runner // 未配置试运行库时为 nil
	Cron    *ingest.CronRunner
	Ticker  *ticker.Ticker
}

// Build 组装全部组件。clock 为 nil 时使用系统时钟。
func Build(c Config, clock engine.Clock) (*Services, error) {
	if clock == nil {
		clock = engine.SystemClock{}
	}
	sqlx.DisableStmtLog() // 只保留慢查询与错误日志
	conn := sqlx.NewMysql(ReadCommitted(c.DataSource))
	s := &Services{Conn: conn, Store: engine.NewStore(conn), Plans: engine.NewPlans(conn), Tasks: task.NewStore(conn), Lark: lark.NewMock()}

	s.Groups = directory.StaticGroups{}
	if c.GroupsFile != "" {
		g, err := directory.LoadGroups(c.GroupsFile)
		if err != nil {
			return nil, err
		}
		s.Groups = g
	}
	s.Authz = directory.StaticPermissions{}
	if c.PermissionsFile != "" {
		p, err := directory.LoadPermissions(c.PermissionsFile)
		if err != nil {
			return nil, err
		}
		s.Authz = p
	}
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
	s.Engine = engine.New(s.Store, s.Plans, nodes.Default(), s.Actions, clock, engine.Options{Owner: owner, DriveParallelism: c.Parallelism})
	if c.DryRunDataSource != "" {
		if c.DryRunDataSource == c.DataSource {
			return nil, fmt.Errorf("bootstrap: DryRunDataSource must not be the platform database")
		}
		readOnly := func(name string) bool { _, ok := skills.Builtin[name]; return ok } // 平台内置 skill 都是只读查询
		s.DryRun = dryrun.New(sqlx.NewMysql(ReadCommitted(c.DryRunDataSource)), s.Actions, nodes.Default(), readOnly)
	}
	s.Cron = ingest.NewCronRunner(conn, s.Plans, s.Tasks, s.Engine, s.Actions, clock)
	s.Ticker = &ticker.Ticker{Cron: s.Cron, Engine: s.Engine}
	return s, nil
}
