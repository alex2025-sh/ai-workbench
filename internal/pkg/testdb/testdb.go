// Package testdb 为集成测试创建临时 MySQL / TiDB 数据库。
//
// 设置环境变量 WB_TEST_MYSQL 后启用，值为不带库名的 DSN，如
//
//	root:@tcp(127.0.0.1:3306)/
//
// 未设置时调用方的测试被跳过。
package testdb

import (
	"database/sql"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// EnvDSN 是启用集成测试的环境变量。
const EnvDSN = "WB_TEST_MYSQL"

// New 创建一个临时数据库并执行给定的 SQL 文件（路径相对仓库根目录），返回带 parseTime=true&loc=UTC 的 DSN。
// 测试结束时删除数据库。
func New(t *testing.T, files ...string) string {
	t.Helper()
	base := os.Getenv(EnvDSN)
	if base == "" {
		t.Skipf("set %s to run MySQL integration tests", EnvDSN)
	}
	base = strings.TrimSuffix(base, "/") + "/"
	admin, err := sql.Open("mysql", base+"?multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := fmt.Sprintf("wbtest_%d_%d", time.Now().UnixNano()%1_000_000_000, rand.Intn(1000))
	if _, err := admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE " + name) })

	db, err := sql.Open("mysql", base+name+"?multiStatements=true&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	root := repoRoot()
	for _, f := range files {
		matches, _ := filepath.Glob(filepath.Join(root, f))
		if len(matches) == 0 {
			t.Fatalf("testdb: no file matches %s", f)
		}
		for _, m := range matches {
			raw, err := os.ReadFile(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(string(raw)); err != nil {
				t.Fatalf("testdb: apply %s: %v", m, err)
			}
		}
	}
	return base + name + "?parseTime=true&loc=UTC&charset=utf8mb4"
}

// Exec 在 dsn 指向的库上执行一条语句。
func Exec(t *testing.T, dsn, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("testdb: %s: %v", query, err)
	}
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}
