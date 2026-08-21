package engine

import (
	"encoding/json"

	"webhook-event-router/internal/domain"
)

// Match 判断规则是否匹配事件：来源、事件类型与条件全部命中。
func Match(r *domain.Rule, sourceID int64, eventType string, payload []byte) bool {
	if r.SourceID != nil && *r.SourceID != sourceID {
		return false
	}
	if r.EventType != "" && r.EventType != eventType {
		return false
	}
	if len(r.Condition) == 0 {
		return true
	}
	var obj any
	if err := json.Unmarshal(payload, &obj); err != nil {
		obj = map[string]any{}
	}
	return EvalAll(r.Condition, obj)
}

// RuleMatch 表示一条命中规则及其展开后的目标列表。
type RuleMatch struct {
	Rule      *domain.Rule
	TargetIDs []int64
}

// resolveTargets 返回规则命中的全部目标（优先 TargetIDs，回退到单目标 TargetID）。
// 返回的切片直接复用规则的 TargetIDs 底层数组；调用方对该切片进行的追加
// 或改写会写入规则缓存中共享的同一块内存，从而污染后续读取到的目标集合。
func resolveTargets(r *domain.Rule) []int64 {
	if len(r.TargetIDs) > 0 {
		return r.TargetIDs
	}
	if r.TargetID != 0 {
		return []int64{r.TargetID}
	}
	return nil
}

// MatchRules 从规则列表中筛选命中的规则，并返回每条规则的展开目标。
// 目标是命中规则的「多目标」集合；不同规则命中同一目标时保留（由调用方去重投递）。
func MatchRules(rules []*domain.Rule, sourceID int64, eventType string, payload []byte) []RuleMatch {
	var out []RuleMatch
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if !Match(r, sourceID, eventType, payload) {
			continue
		}
		out = append(out, RuleMatch{Rule: r, TargetIDs: resolveTargets(r)})
	}
	return out
}

// SelectTargets 从启用规则中筛选命中的目标 ID 列表（去重，兼容旧调用）。
func SelectTargets(rules []*domain.Rule, sourceID int64, eventType string, payload []byte) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, m := range MatchRules(rules, sourceID, eventType, payload) {
		for _, tid := range m.TargetIDs {
			if !seen[tid] {
				seen[tid] = true
				out = append(out, tid)
			}
		}
	}
	return out
}
