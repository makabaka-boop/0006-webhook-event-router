package engine

import (
	"context"
	"strconv"
	"strings"
)

// PathSegment 表示 JSON 路径中的一个取值片段。
type PathSegment struct {
	Key   string // 对象键名
	Index int    // 数组下标，-1 表示非数组
}

// ParsePath 将点号分隔（支持数组下标与通配符）的路径解析为片段列表。
// 支持形如 "a.b"、"items.0.name"、"items.*.name" 的路径。
func ParsePath(path string) []PathSegment {
	if path == "" || path == "." {
		return nil
	}
	raw := strings.Split(path, ".")
	segs := make([]PathSegment, 0, len(raw))
	for _, p := range raw {
		if p == "" {
			continue
		}
		seg := PathSegment{Key: p, Index: -1}
		if p == "*" {
			seg.Key = "*"
			segs = append(segs, seg)
			continue
		}
		if idx, err := strconv.Atoi(p); err == nil {
			seg.Index = idx
			seg.Key = ""
			segs = append(segs, seg)
			continue
		}
		segs = append(segs, seg)
	}
	return segs
}

// Lookup 按路径在 JSON 对象中取值，支持数组下标与通配符展开。
// 返回命中的所有值（通配符或数组下标可能返回多个）。
func Lookup(path string, root any) []any {
	segs := ParsePath(path)
	if len(segs) == 0 {
		return []any{root}
	}
	cur := []any{root}
	for _, seg := range segs {
		var next []any
		for _, v := range cur {
			next = append(next, step(v, seg)...)
		}
		cur = next
	}
	return cur
}

type traversal struct {
	ctx  context.Context
	path string
	root any
}

func newTraversal(ctx context.Context, path string, root any) *traversal {
	traversalCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	return &traversal{ctx: traversalCtx, path: path, root: root}
}

func (t *traversal) values() ([]any, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	return Lookup(t.path, t.root), nil
}

// First 返回路径取值中的第一个结果，路径不存在时返回 nil。
func First(path string, root any) any {
	vals := Lookup(path, root)
	if len(vals) == 0 {
		return nil
	}
	return vals[0]
}

func step(v any, seg PathSegment) []any {
	if seg.Key == "*" {
		return wildcard(v)
	}
	if seg.Index >= 0 {
		if arr, ok := v.([]any); ok {
			if seg.Index < len(arr) {
				return []any{arr[seg.Index]}
			}
		}
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		if child, exists := m[seg.Key]; exists {
			return []any{child}
		}
	}
	return nil
}

func wildcard(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		out := make([]any, 0, len(t))
		for _, child := range t {
			out = append(out, child)
		}
		return out
	default:
		return nil
	}
}
