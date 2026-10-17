// Package yamljson 把 YAML 转成 JSON。
//
// 按 YAML 1.2 解析：yes / no / on / off / y / n 都是字符串，只有 true / false 是布尔值，
// 避免节点 ID、端口名被误解析成布尔值。
package yamljson

import (
	"encoding/json"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// Convert 把一份 YAML 文档转成 JSON。映射的键必须是字符串。
func Convert(raw []byte) ([]byte, error) {
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	out, err := normalize(v, "")
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func normalize(v any, path string) (any, error) {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			n, err := normalize(val, path+"."+k)
			if err != nil {
				return nil, err
			}
			m[k] = n
		}
		return m, nil
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("yamljson: non-string key %v at %q", k, path)
			}
			n, err := normalize(val, path+"."+ks)
			if err != nil {
				return nil, err
			}
			m[ks] = n
		}
		return m, nil
	case []any:
		s := make([]any, len(x))
		for i, val := range x {
			n, err := normalize(val, fmt.Sprintf("%s.%d", path, i))
			if err != nil {
				return nil, err
			}
			s[i] = n
		}
		return s, nil
	default:
		return x, nil
	}
}
