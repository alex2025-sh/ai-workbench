// Command wbctl 是任务类型配置的命令行工具：在没有前端时用 YAML 编写、校验任务类型，
// 并导出提交给 SaveTaskTypeDraft 的 JSON。
//
//	wbctl validate FILE...   校验（YAML 或 JSON），有 error 时退出码为 1
//	wbctl json FILE          输出 body_json
//	wbctl plan FILE          输出编译后的执行计划
//	wbctl node-types         输出节点类型目录（含配置 schema），即 CatalogService.ListNodeTypes 的内容
//	wbctl schema             输出 task_type.schema.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apischema "github.com/alex2025-sh/ai-workbench/api/schema"
	"github.com/alex2025-sh/ai-workbench/internal/flow/compiler"
	"github.com/alex2025-sh/ai-workbench/internal/flow/dsl"
	"github.com/alex2025-sh/ai-workbench/internal/spec"
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
		err = withBody(os.Args[2:], func(body []byte) error { return writeJSON(json.RawMessage(body)) })
	case "plan":
		err = withBody(os.Args[2:], func(body []byte) error {
			res := spec.Check(body)
			if !res.OK() {
				printIssues(os.Args[2], res.Issues)
				return fmt.Errorf("compile failed")
			}
			return writeJSON(res.Plan)
		})
	case "node-types":
		err = nodeTypes()
	case "schema":
		_, err = os.Stdout.Write(apischema.TaskTypeSchema())
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "wbctl:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wbctl validate FILE... | json FILE | plan FILE | node-types | schema")
	os.Exit(2)
}

func readBody(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return spec.FromYAML(raw)
	default:
		return raw, nil
	}
}

func withBody(args []string, fn func([]byte) error) error {
	if len(args) != 1 {
		usage()
	}
	body, err := readBody(args[0])
	if err != nil {
		return err
	}
	return fn(body)
}

func validate(files []string) error {
	if len(files) == 0 {
		usage()
	}
	failed := 0
	for _, f := range files {
		body, err := readBody(f)
		if err != nil {
			return err
		}
		res := spec.Check(body)
		printIssues(f, res.Issues)
		if res.OK() {
			fmt.Printf("%s: ok (%d nodes, %d warnings)\n", f, len(res.Plan.Order), len(res.Issues))
		} else {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d files have errors", failed, len(files))
	}
	return nil
}

func printIssues(file string, issues []compiler.Issue) {
	for _, i := range issues {
		fmt.Printf("%s: %s %s [%s] %s\n", file, i.Severity, i.Path, i.Code, i.Message)
	}
}

type nodeTypeOut struct {
	Type         string          `json:"type"`
	Category     string          `json:"category"`
	DisplayName  string          `json:"display_name"`
	Description  string          `json:"description"`
	PortRule     string          `json:"port_rule"`
	FixedPorts   []string        `json:"fixed_ports,omitempty"`
	ConfigSchema json.RawMessage `json:"config_schema"`
}

func nodeTypes() error {
	var out []nodeTypeOut
	for _, t := range dsl.NodeTypes() {
		s, err := apischema.NodeConfigSchema(string(t.Type))
		if err != nil {
			return err
		}
		out = append(out, nodeTypeOut{
			Type: string(t.Type), Category: string(t.Category), DisplayName: t.DisplayName, Description: t.Description,
			PortRule: string(t.PortRule), FixedPorts: t.FixedPorts, ConfigSchema: s,
		})
	}
	return writeJSON(out)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
