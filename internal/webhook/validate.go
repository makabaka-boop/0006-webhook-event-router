package webhook

import (
	"encoding/json"
	"strings"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
)

// ValidatePayload 校验负载为合法 JSON 对象且不超过大小限制。
func ValidatePayload(payload []byte, maxSize int64) error {
	if int64(len(payload)) > maxSize {
		return errs.New(errs.CodePayloadTooLarge, "payload exceeds maximum size")
	}
	if strings.TrimSpace(string(payload)) == "" {
		return errs.New(errs.CodePayloadInvalid, "payload is empty")
	}
	var v any
	if err := json.Unmarshal(payload, &v); err != nil {
		return errs.New(errs.CodePayloadInvalid, "payload is not valid JSON")
	}
	if m, ok := v.(map[string]any); !ok || m == nil {
		return errs.New(errs.CodePayloadInvalid, "payload must be a JSON object")
	}
	return nil
}

// ValidateEventType 校验事件类型是否在来源允许列表内。
func ValidateEventType(src *domain.Source, eventType string) error {
	if len(src.AllowedTypes) == 0 {
		return nil
	}
	for _, t := range src.AllowedTypes {
		if t == eventType {
			return nil
		}
	}
	return errs.New(errs.CodeEventTypeNotAllowed, "event type not allowed for source")
}
