package sqlite

import (
	"context"
	"reflect"
	"testing"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/engine"
)

func TestRulesKeepDistinctTargetAssociations(t *testing.T) {
	ctx := context.Background()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	store := New(db)

	source := &domain.Source{
		Name:         "enabled-source",
		Secret:       "test-secret",
		Enabled:      true,
		AllowedTypes: []string{"push"},
	}
	if err := store.CreateSource(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}

	targetA := &domain.Target{Name: "target-a", URL: "https://target-a.invalid/webhook", Enabled: true}
	targetB := &domain.Target{Name: "target-b", URL: "https://target-b.invalid/webhook", Enabled: true}
	for _, target := range []*domain.Target{targetA, targetB} {
		if err := store.CreateTarget(ctx, target); err != nil {
			t.Fatalf("create %s: %v", target.Name, err)
		}
	}

	sourceID := source.ID
	ruleA := &domain.Rule{
		Name:      "rule-a",
		SourceID:  &sourceID,
		EventType: "push",
		Enabled:   true,
		Priority:  2,
		TargetID:  targetA.ID,
		TargetIDs: []int64{targetA.ID},
	}
	ruleB := &domain.Rule{
		Name:      "rule-b",
		SourceID:  &sourceID,
		EventType: "push",
		Enabled:   true,
		Priority:  1,
		TargetID:  targetB.ID,
		TargetIDs: []int64{targetB.ID},
	}
	if err := store.CreateRule(ctx, ruleA); err != nil {
		t.Fatalf("create rule-a: %v", err)
	}
	if err := store.CreateRule(ctx, ruleB); err != nil {
		t.Fatalf("create rule-b: %v", err)
	}

	gotA, err := store.ListRuleTargets(ctx, ruleA.ID)
	if err != nil {
		t.Fatalf("list targets for rule-a: %v", err)
	}
	gotB, err := store.ListRuleTargets(ctx, ruleB.ID)
	if err != nil {
		t.Fatalf("list targets for rule-b: %v", err)
	}
	if !reflect.DeepEqual(gotA, []int64{targetA.ID}) {
		t.Fatalf("rule-a targets = %v, want [%d]", gotA, targetA.ID)
	}
	if !reflect.DeepEqual(gotB, []int64{targetB.ID}) {
		t.Fatalf("rule-b targets = %v, want [%d]", gotB, targetB.ID)
	}

	rules, err := store.ListEnabledRules(ctx)
	if err != nil {
		t.Fatalf("list enabled rules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("enabled rules = %d, want 2", len(rules))
	}
	if !reflect.DeepEqual(rules[0].TargetIDs, []int64{targetA.ID}) || !reflect.DeepEqual(rules[1].TargetIDs, []int64{targetB.ID}) {
		t.Fatalf("listed rule targets = [%v %v], want [[%d] [%d]]", rules[0].TargetIDs, rules[1].TargetIDs, targetA.ID, targetB.ID)
	}

	matches := engine.MatchRules(rules, source.ID, "push", []byte(`{"event":"new-unique-event"}`))
	if len(matches) != 2 {
		t.Fatalf("matching rules = %d, want 2", len(matches))
	}
	if !reflect.DeepEqual(matches[0].TargetIDs, []int64{targetA.ID}) || !reflect.DeepEqual(matches[1].TargetIDs, []int64{targetB.ID}) {
		t.Fatalf("matched target sets = [%v %v], want [[%d] [%d]]", matches[0].TargetIDs, matches[1].TargetIDs, targetA.ID, targetB.ID)
	}
	selected := engine.SelectTargets(rules, source.ID, "push", []byte(`{"event":"new-unique-event"}`))
	if !reflect.DeepEqual(selected, []int64{targetA.ID, targetB.ID}) {
		t.Fatalf("selected target IDs = %v, want [%d %d]", selected, targetA.ID, targetB.ID)
	}
}
