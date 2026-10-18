// Package cursor 编码键集分页的游标：上一页最后一行的排序键用 | 连接后 base64，对调用方不透明。
package cursor

import (
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
)

// ErrBad 表示游标无法解析。
var ErrBad = errors.New("cursor: bad cursor")

// Default 与 Max 约束列表的每页条数。
const (
	Default = 20
	Max     = 100
)

// Size 把请求的每页条数规整到 [1, Max]，0 或负数取 Default。
func Size(n int) int {
	switch {
	case n <= 0:
		return Default
	case n > Max:
		return Max
	default:
		return n
	}
}

// Encode 编码排序键。
func Encode(keys ...int64) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = strconv.FormatInt(k, 10)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "|")))
}

// Decode 解码出 n 个排序键。
func Decode(s string, n int) ([]int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrBad
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != n {
		return nil, ErrBad
	}
	out := make([]int64, n)
	for i, p := range parts {
		if out[i], err = strconv.ParseInt(p, 10, 64); err != nil {
			return nil, ErrBad
		}
	}
	return out, nil
}
