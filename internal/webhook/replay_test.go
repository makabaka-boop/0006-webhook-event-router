package webhook

import (
	"testing"
	"time"
)

func TestValidateTimestamp(t *testing.T) {
	now := time.Now().UTC()
	window := 300 * time.Second

	// 当前时间应通过
	if err := ValidateTimestamp(now, now, window); err != nil {
		t.Fatalf("current timestamp should pass: %v", err)
	}
	// 未来时间应拒绝
	if err := ValidateTimestamp(now.Add(window+time.Second), now, window); err == nil {
		t.Fatal("future timestamp should be rejected")
	}
	// 过旧时间应拒绝
	if err := ValidateTimestamp(now.Add(-window-time.Second), now, window); err == nil {
		t.Fatal("stale timestamp should be rejected")
	}
	// 零值应拒绝
	if err := ValidateTimestamp(time.Time{}, now, window); err == nil {
		t.Fatal("zero timestamp should be rejected")
	}
}

func TestParseTimestamp(t *testing.T) {
	ts, ok := ParseTimestamp(1700000000)
	if !ok {
		t.Fatal("expected valid parse")
	}
	if ts.Unix() != 1700000000 {
		t.Fatalf("expected unix 1700000000, got %d", ts.Unix())
	}
	if _, ok := ParseTimestamp(0); ok {
		t.Fatal("expected invalid for zero")
	}
	if _, ok := ParseTimestamp(-5); ok {
		t.Fatal("expected invalid for negative")
	}
}
