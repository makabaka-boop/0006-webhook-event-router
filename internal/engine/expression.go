package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"webhook-event-router/internal/domain"
)

// Eval 求值一个条件节点；节点为 and/or/not 分组时递归求值子条件，
// 否则按叶子运算符对路径取值进行匹配。
func Eval(c domain.Condition, root any) bool {
	switch c.Op {
	case "and":
		for _, child := range c.Children {
			if !Eval(child, root) {
				return false
			}
		}
		return true
	case "or":
		for _, child := range c.Children {
			if Eval(child, root) {
				return true
			}
		}
		return false
	case "not":
		for _, child := range c.Children {
			if Eval(child, root) {
				return false
			}
		}
		return true
	default:
		return evalLeaf(c, root)
	}
}

// EvalAll 判断一组条件是否全部命中（顶级条件之间为 and 关系）。
func EvalAll(conds []domain.Condition, root any) bool {
	for _, c := range conds {
		if !Eval(c, root) {
			return false
		}
	}
	return true
}

func evalLeaf(c domain.Condition, root any) bool {
	vals := Lookup(c.Path, root)
	switch c.Op {
	case "exists":
		return exists(c.Path, root)
	case "in":
		return in(vals, c.Value)
	case "not_in":
		return !in(vals, c.Value)
	case "regex":
		return regexMatch(vals, c.Value)
	case "eq", "ne", "gt", "gte", "lt", "lte", "contains":
		return evalCompare(c.Op, vals, c.Value)
	default:
		return false
	}
}

func exists(path string, root any) bool {
	return len(Lookup(path, root)) > 0 && First(path, root) != nil
}

func in(vals []any, value any) bool {
	list, ok := asList(value)
	if !ok {
		return false
	}
	for _, v := range vals {
		for _, item := range list {
			if equal(v, item) {
				return true
			}
		}
	}
	return false
}

func asList(value any) ([]any, bool) {
	switch t := value.(type) {
	case []any:
		return t, true
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out, true
	case string:
		// 单个字符串直接作为单元素列表，方便 in 运算符使用标量右值。
		return []any{t}, false
	default:
		return nil, false
	}
}

func regexMatch(vals []any, value any) bool {
	pattern, ok := value.(string)
	if !ok || pattern == "" {
		return false
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	for _, v := range vals {
		if re.MatchString(normalize(v)) {
			return true
		}
	}
	return false
}

func evalCompare(op string, vals []any, value any) bool {
	// 未命中任何值的路径，仅 ne 视为真（值不存在即不等于）。
	if len(vals) == 0 {
		return op == "ne"
	}
	matched := false
	for _, v := range vals {
		switch op {
		case "eq":
			matched = matched || equal(v, value)
		case "ne":
			if equal(v, value) {
				return false
			}
			matched = true
		case "gt":
			matched = matched || compare(v, value) > 0
		case "gte":
			matched = matched || compare(v, value) >= 0
		case "lt":
			matched = matched || compare(v, value) < 0
		case "lte":
			matched = matched || compare(v, value) <= 0
		case "contains":
			matched = matched || strings.Contains(fmt.Sprint(v), fmt.Sprint(value))
		}
	}
	return matched
}

func equal(a, b any) bool {
	return normalize(a) == normalize(b)
}

func normalize(v any) string {
	switch n := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(n)
	case string:
		return n
	case jsonNumber:
		return n.String()
	default:
		return fmt.Sprint(v)
	}
}

// jsonNumber 保留 JSON 大整数精度以避免 float64 转换失真。
type jsonNumber string

func (n jsonNumber) String() string { return string(n) }

func compare(a, b any) int {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		switch {
		case af > bf:
			return 1
		case af < bf:
			return -1
		default:
			return 0
		}
	}
	as, bs := normalize(a), normalize(b)
	return strings.Compare(as, bs)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	case jsonNumber:
		f, err := strconv.ParseFloat(string(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
