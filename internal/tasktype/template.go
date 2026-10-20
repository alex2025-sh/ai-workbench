package tasktype

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// placeholder 是模板中的字段占位：{字段名}。{key} 是数据源给出的 key。
var placeholder = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

// KeyPlaceholder 是 key 列在模板中的名字。
const KeyPlaceholder = "key"

// Placeholders 返回模板引用的字段名（按出现顺序，去重）。
func Placeholders(tpl string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range placeholder.FindAllStringSubmatch(tpl, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// Render 把模板中的占位替换为 value 返回的值；value 返回 false 的占位替换为空。
func Render(tpl string, value func(name string) (string, bool)) string {
	return placeholder.ReplaceAllStringFunc(tpl, func(m string) string {
		v, _ := value(m[1 : len(m)-1])
		return v
	})
}

// ParseDuration 解析时长，在 time.ParseDuration 的基础上支持 d（天）：1d、1d12h。
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	var total time.Duration
	rest := s
	for rest != "" {
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		n, err := strconv.Atoi(rest[:i])
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		rest = rest[i:]
		unit := ""
		for _, u := range []string{"ms", "s", "m", "h", "d"} {
			if strings.HasPrefix(rest, u) {
				unit = u
				break
			}
		}
		switch unit {
		case "ms":
			total += time.Duration(n) * time.Millisecond
		case "s":
			total += time.Duration(n) * time.Second
		case "m":
			total += time.Duration(n) * time.Minute
		case "h":
			total += time.Duration(n) * time.Hour
		case "d":
			total += time.Duration(n) * 24 * time.Hour
		default:
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		rest = rest[len(unit):]
	}
	if total <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return total, nil
}
