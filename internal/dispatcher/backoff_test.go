package dispatcher

import (
	"testing"
	"time"
)

func TestBackoffBounds(t *testing.T) {
	b := NewBackoff(10 * time.Millisecond)
	// 第 1 次重试：基数 + 抖动，至少为基数。
	if d := b.Duration(1); d < 10*time.Millisecond {
		t.Fatalf("attempt 1 backoff too small: %v", d)
	}
	// 上限为 16 倍基数 + 抖动（抖动上限为基数）。
	maxCap := 16*10*time.Millisecond + 10*time.Millisecond
	if d := b.Duration(100); d > maxCap {
		t.Fatalf("backoff exceeded cap: %v", d)
	}
	// 指数增长的下界：每次退避（含抖动）都不小于其指数基数部分。
	for attempt := 1; attempt <= 6; attempt++ {
		d := b.Duration(attempt)
		if bare := baseMultiplier(b, attempt); bare > d {
			t.Fatalf("attempt %d: %v below bare base %v", attempt, d, bare)
		}
	}
}

func baseMultiplier(b *Backoff, attempt int) time.Duration {
	mul := 1
	for i := 1; i < attempt && mul < b.maxMul; i++ {
		mul *= 2
	}
	return b.base * time.Duration(mul)
}
