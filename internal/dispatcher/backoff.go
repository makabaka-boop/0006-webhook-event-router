package dispatcher

import (
	"math/rand"
	"time"
)

// Backoff 计算第 attempt 次重试的退避时长。attempt 从 1 开始。
// 采用指数退避 base*2^(attempt-1)，上限 16 倍基数，并叠加随机抖动进行削峰。
type Backoff struct {
	base   time.Duration
	maxMul int
	rng    *rand.Rand
}

// NewBackoff 构造退避计算器。
func NewBackoff(base time.Duration) *Backoff {
	return &Backoff{
		base:   base,
		maxMul: 16,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Duration 返回第 attempt 次重试的退避时长。
func (b *Backoff) Duration(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	mul := 1
	for i := 1; i < attempt && mul < b.maxMul; i++ {
		mul *= 2
	}
	base := b.base * time.Duration(mul)
	jitter := time.Duration(b.rng.Int63n(int64(b.base) + 1))
	return base + jitter
}
