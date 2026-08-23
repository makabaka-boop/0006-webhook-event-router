package engine

import (
	"encoding/json"
	"testing"

	"webhook-event-router/internal/domain"
)

func parse(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return v
}

func cond(op, path string, value any) domain.Condition {
	return domain.Condition{Op: op, Path: path, Value: value}
}

func group(op string, children ...domain.Condition) domain.Condition {
	return domain.Condition{Op: op, Children: children}
}

func TestExpressionOperators(t *testing.T) {
	root := parse(t, `{"repo":"webhook-router","stars":10,"labels":["go","api"],"owner":{"name":"alice"}}`)
	cases := []struct {
		name string
		c    domain.Condition
		want bool
	}{
		{"eq", cond("eq", "repo", "webhook-router"), true},
		{"ne", cond("ne", "repo", "other"), true},
		{"gt", cond("gt", "stars", 5), true},
		{"gte", cond("gte", "stars", 10), true},
		{"lt", cond("lt", "stars", 20), true},
		{"lte", cond("lte", "stars", 10), true},
		{"regex", cond("regex", "repo", "^webhook-"), true},
		{"regex_miss", cond("regex", "repo", "^x$"), false},
		{"in", cond("in", "repo", []any{"webhook-router", "other"}), true},
		{"in_miss", cond("in", "repo", []any{"a", "b"}), false},
		{"exists", cond("exists", "owner.name", nil), true},
		{"exists_miss", cond("exists", "owner.missing", nil), false},
		{"contains", cond("contains", "repo", "hook"), true},
	}
	for _, c := range cases {
		if got := Eval(c.c, root); got != c.want {
			t.Errorf("%s: Eval()=%v want %v", c.name, got, c.want)
		}
	}
}

func TestExpressionGroups(t *testing.T) {
	root := parse(t, `{"stars":10,"repo":"webhook-router"}`)
	and := group("and", cond("gt", "stars", 5), cond("eq", "repo", "webhook-router"))
	if !Eval(and, root) {
		t.Fatal("expected and group true")
	}
	andMiss := group("and", cond("gt", "stars", 5), cond("eq", "repo", "other"))
	if Eval(andMiss, root) {
		t.Fatal("expected and group false")
	}
	or := group("or", cond("eq", "repo", "x"), cond("gt", "stars", 5))
	if !Eval(or, root) {
		t.Fatal("expected or group true")
	}
	not := group("not", cond("eq", "repo", "other"))
	if !Eval(not, root) {
		t.Fatal("expected not group true")
	}
}

func TestNestedGroups(t *testing.T) {
	root := parse(t, `{"level":"error","count":100,"env":"prod"}`)
	c := group("and",
		group("or", cond("eq", "level", "error"), cond("eq", "level", "warn")),
		group("not", cond("eq", "env", "dev")),
	)
	if !Eval(c, root) {
		t.Fatal("expected nested group true")
	}
}

func TestPathArrays(t *testing.T) {
	root := parse(t, `{"items":[{"n":1},{"n":2},{"n":3}]}`)
	// 数组下标取值
	if got := First("items.1.n", root); got != float64(2) {
		t.Fatalf("items.1.n = %v want 2", got)
	}
	// 通配符命中所有 n
	vals := Lookup("items.*.n", root)
	if len(vals) != 3 {
		t.Fatalf("wildcard returned %d values want 3", len(vals))
	}
}

func TestMatchMultiTarget(t *testing.T) {
	payload := []byte(`{"repo":"webhook-router"}`)
	rules := []*domain.Rule{
		{ID: 1, Enabled: true, TargetID: 10, TargetIDs: []int64{10, 20}, Condition: []domain.Condition{cond("eq", "repo", "webhook-router")}},
	}
	got := MatchRules(rules, 0, "", payload)
	if len(got) != 1 {
		t.Fatalf("expected 1 match, got %d", len(got))
	}
	if len(got[0].TargetIDs) != 2 {
		t.Fatalf("expected 2 targets, got %v", got[0].TargetIDs)
	}
}
