package webhook

import (
	"time"

	"webhook-event-router/internal/errs"
)

// ValidateTimestamp 做时间戳防重放校验。
// ts 为请求携带的事件发生时间（unix 秒），now 为服务端当前时间，
// window 为允许的时钟偏差窗口。时间戳缺失、在未来、或早于窗口均视为重放。
func ValidateTimestamp(ts, now time.Time, window time.Duration) error {
	if ts.IsZero() {
		return errs.New(errs.CodeReplayRejected, "missing or invalid timestamp")
	}
	if ts.After(now.Add(window)) {
		return errs.New(errs.CodeReplayRejected, "timestamp is in the future")
	}
	if now.Sub(ts) > window {
		return errs.New(errs.CodeReplayRejected, "timestamp is too old")
	}
	return nil
}

// ParseTimestamp 解析可选的事件时间戳。合法时返回时间与 true，否则返回零值与 false。
func ParseTimestamp(unixSeconds int64) (time.Time, bool) {
	if unixSeconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(unixSeconds, 0).UTC(), true
}
