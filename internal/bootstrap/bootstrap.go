// Package bootstrap 组装配置中心、任务中心、数据源、通知与 ticker，供 app/rpc、app/worker、命令行与端到端测试共用。
package bootstrap

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"github.com/alex2025-sh/ai-workbench/internal/assign"
	"github.com/alex2025-sh/ai-workbench/internal/directory"
	"github.com/alex2025-sh/ai-workbench/internal/model"
	"github.com/alex2025-sh/ai-workbench/internal/notify"
	"github.com/alex2025-sh/ai-workbench/internal/registry"
	"github.com/alex2025-sh/ai-workbench/internal/source"
	"github.com/alex2025-sh/ai-workbench/internal/task"
	"github.com/alex2025-sh/ai-workbench/internal/ticker"
)

// Config 是组装所需的配置。
type Config struct {
	DataSource string // 平台库，需要 parseTime=true&loc=UTC
	// Datasources 是 SQL 来源可用的只读连接：名字 → DSN（只读从库账号）。
	Datasources     map[string]string `json:",optional"`
	RelationsFile   string            `json:",optional"` // 本地业务关系表（如 deploy/mock/vip_rm.yaml）
	PermissionsFile string            `json:",optional"` // 本地接口级权限（如 deploy/mock/permissions.yaml）
	WebBase         string            `json:",optional"` // 工作台地址，Lark 卡片的「在工作台处理」按钮指向 WebBase/tasks/:id
}

// ReadCommitted 在 DSN 上加上 READ-COMMITTED 隔离级别（已指定时不改），避免间隙锁。
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
	Conn        sqlx.SqlConn
	Registry    *registry.Registry
	Assign      *assign.Service
	Tasks       *task.Store
	Sources     *source.Runner
	Datasources source.Datasources
	Relations   *assign.StaticRelations
	Authz       directory.Authorizer
	Lark        *notify.Mock
	Notify      *notify.Dispatcher
	Ticker      *ticker.Ticker
}

// Build 组装全部组件。now 为 nil 时使用系统时钟。
func Build(c Config, now func() time.Time) (*Services, error) {
	if now == nil {
		now = time.Now
	}
	sqlx.DisableStmtLog()
	conn := sqlx.NewMysql(ReadCommitted(c.DataSource))
	s := &Services{Conn: conn, Lark: &notify.Mock{}, Datasources: source.Datasources{}}

	for name, dsn := range c.Datasources {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, fmt.Errorf("bootstrap: datasource %s: %w", name, err)
		}
		db.SetMaxOpenConns(4)
		s.Datasources[name] = db
	}
	s.Relations = &assign.StaticRelations{}
	if c.RelationsFile != "" {
		r, err := assign.LoadRelations(c.RelationsFile)
		if err != nil {
			return nil, err
		}
		s.Relations = r
	}
	s.Authz = directory.StaticPermissions{}
	if c.PermissionsFile != "" {
		p, err := directory.LoadPermissions(c.PermissionsFile)
		if err != nil {
			return nil, err
		}
		s.Authz = p
	}
	s.Assign = assign.New(s.Relations)
	s.Registry = registry.New(conn, refs{s})
	s.Notify = &notify.Dispatcher{Outbox: model.NewOutboxModel(conn), Sender: s.Lark, Now: now}
	s.Tasks = task.NewStore(conn, s.Registry, s.Notify, now)
	if c.WebBase != "" {
		base := strings.TrimSuffix(c.WebBase, "/")
		s.Tasks.TaskURL = func(id int64) string { return fmt.Sprintf("%s/tasks/%d", base, id) }
	}
	s.Sources = &source.Runner{Registry: s.Registry, Datasources: s.Datasources, Tasks: s.Tasks, Assign: s.Assign,
		States: model.NewSourceStateModel(conn), Now: now}
	s.Ticker = &ticker.Ticker{Sources: s.Sources, Tasks: s.Tasks, Notify: s.Notify}
	return s, nil
}

// refs 实现 registry.Refs。
type refs struct{ s *Services }

func (r refs) DatasourceExists(name string) bool { return r.s.Datasources.Exists(name) }
func (r refs) RelationExists(name string) bool {
	for _, n := range r.s.Relations.Names() {
		if n == name {
			return true
		}
	}
	return false
}
