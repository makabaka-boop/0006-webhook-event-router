package engine

import (
	"testing"

	"webhook-event-router/internal/domain"
)

func intPtr(v int64) *int64 { return &v }

func TestMatch(t *testing.T) {
	payload := []byte(`{"repo":"webhook-router","stars":10,"tags":["go","api"]}`)
	rule := &domain.Rule{
		SourceID:  intPtr(1),
		EventType: "push",
		Condition: []domain.Condition{{Path: "repo", Op: "eq", Value: "webhook-router"}},
	}
	if !Match(rule, 1, "push", payload) {
		t.Fatal("expected match")
	}
	if Match(rule, 2, "push", payload) {
		t.Fatal("expected no match on source")
	}
	if Match(rule, 1, "pull_request", payload) {
		t.Fatal("expected no match on event type")
	}
	miss := &domain.Rule{Condition: []domain.Condition{{Path: "repo", Op: "eq", Value: "other"}}}
	if Match(miss, 1, "push", payload) {
		t.Fatal("expected no match on condition")
	}
}

func TestSelectTargets(t *testing.T) {
	payload := []byte(`{"repo":"webhook-router"}`)
	rules := []*domain.Rule{
		{ID: 1, TargetID: 10, Enabled: true, Condition: []domain.Condition{{Path: "repo", Op: "eq", Value: "webhook-router"}}},
		{ID: 2, TargetID: 20, Enabled: true, Condition: []domain.Condition{{Path: "repo", Op: "eq", Value: "other"}}},
		{ID: 3, TargetID: 10, Enabled: true},
		{ID: 4, TargetID: 30, Enabled: false},
	}
	got := SelectTargets(rules, 0, "", payload)
	if len(got) != 1 || got[0] != 10 {
		t.Fatalf("expected [10], got %v", got)
	}
}

func TestMatchRulesDoesNotAliasRuleTargets(t *testing.T) {
	rule := &domain.Rule{ID: 1, Enabled: true, TargetIDs: []int64{10, 20}}
	matches := MatchRules([]*domain.Rule{rule}, 0, "", []byte(`{}`))
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if len(matches[0].TargetIDs) != 2 {
		t.Fatalf("expected 2 targets, got %v", matches[0].TargetIDs)
	}
	// 改写返回切片的元素不应波及规则自身的目标缓存。
	matches[0].TargetIDs[0] = 99
	if rule.TargetIDs[0] != 10 {
		t.Fatalf("MatchRules aliased rule.TargetIDs: rule target mutated to %v", rule.TargetIDs)
	}
}
