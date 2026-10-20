// Command wbctl 是任务类型的命令行工具：
//
//	wbctl validate FILE...              校验任务类型（YAML 或 JSON）：结构与语义，不检查团队与数据源是否存在
//	wbctl json FILE                     把 YAML 转成任务类型 JSON 输出（即前端提交的同一种 JSON）
//	wbctl publish -f CONFIG FILE...     直接发布到平台库（本地开发用；会检查团队、数据源连接与业务关系）
//
// CONFIG 为 app/worker 的配置文件（只用其中的平台库、数据源连接与关系表）。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/conf"

	"github.com/alex2025-sh/ai-workbench/internal/bootstrap"
	"github.com/alex2025-sh/ai-workbench/internal/tasktype"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "validate":
		err = validate(os.Args[2:])
	case "json":
		err = toJSON(os.Args[2:])
	case "publish":
		err = publish(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wbctl validate FILE... | json FILE | publish -f CONFIG FILE...")
	os.Exit(2)
}

// read 读取任务类型文件：.yaml / .yml 转成 JSON，其余按 JSON 读取。
func read(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return tasktype.FromYAML(raw)
	}
	return raw, nil
}

func validate(files []string) error {
	if len(files) == 0 {
		usage()
	}
	failed := 0
	for _, f := range files {
		body, err := read(f)
		if err != nil {
			return err
		}
		_, issues := tasktype.Validate(body)
		if len(issues) == 0 {
			fmt.Printf("ok    %s\n", f)
			continue
		}
		failed++
		fmt.Printf("FAIL  %s\n", f)
		for _, i := range issues {
			fmt.Printf("      %s\n", i)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files have issues", failed, len(files))
	}
	return nil
}

func toJSON(files []string) error {
	if len(files) != 1 {
		usage()
	}
	body, err := read(files[0])
	if err != nil {
		return err
	}
	t, issues := tasktype.Validate(body)
	if len(issues) > 0 {
		return fmt.Errorf("%s has issues: %v", files[0], issues)
	}
	out, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func publish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	config := fs.String("f", "app/worker/etc/worker.yaml", "worker config file")
	actor := fs.String("as", "wbctl", "publisher recorded on the version")
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		usage()
	}
	var c struct {
		bootstrap.Config
		Log    any `json:",optional"`
		Ticker any `json:",optional"`
	}
	conf.MustLoad(*config, &c)
	s, err := bootstrap.Build(c.Config, nil)
	if err != nil {
		return err
	}
	for _, f := range fs.Args() {
		body, err := read(f)
		if err != nil {
			return err
		}
		v, issues, err := s.Registry.PublishBody(context.Background(), body, *actor, time.Now())
		if err != nil {
			return fmt.Errorf("%s: %w %v", f, err, issues)
		}
		fmt.Printf("published %s@%d\n", v.Key, v.Version)
	}
	return nil
}
