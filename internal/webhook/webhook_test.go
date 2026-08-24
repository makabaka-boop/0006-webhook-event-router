package webhook

import (
	"testing"

	"webhook-event-router/internal/domain"
)

func TestSignVerify(t *testing.T) {
	body := []byte(`{"a":1}`)
	sig := Sign("secret", body)
	if !Verify("secret", sig, body) {
		t.Fatal("expected valid signature")
	}
	if Verify("secret", sig, []byte(`{"a":2}`)) {
		t.Fatal("expected signature mismatch")
	}
	if Verify("", sig, body) {
		t.Fatal("empty secret must not verify")
	}
}

func TestValidatePayload(t *testing.T) {
	if err := ValidatePayload([]byte(`{"a":1}`), 1<<20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidatePayload([]byte(`not-json`), 1<<20); err == nil {
		t.Fatal("expected error for non-JSON")
	}
	if err := ValidatePayload([]byte(`[1,2]`), 1<<20); err == nil {
		t.Fatal("expected error for non-object")
	}
	if err := ValidatePayload([]byte(`{"a":1}`), 2); err == nil {
		t.Fatal("expected error for oversized payload")
	}
}

func TestValidateEventType(t *testing.T) {
	src := &domain.Source{AllowedTypes: []string{"push", "issue"}}
	if err := ValidateEventType(src, "push"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateEventType(src, "other"); err == nil {
		t.Fatal("expected error for disallowed type")
	}
	open := &domain.Source{}
	if err := ValidateEventType(open, "anything"); err != nil {
		t.Fatalf("empty allowlist should allow all, got %v", err)
	}
}
