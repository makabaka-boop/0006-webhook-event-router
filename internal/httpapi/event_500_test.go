package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/webhook"
)

// TestCreateEventUnresolvableTargetReturns500 验证事件创建在匹配与投递阶段
// 遇到无法 DNS 解析的目标主机时，客户端应收到 HTTP 500 且事件已持久化。
func TestCreateEventUnresolvableTargetReturns500(t *testing.T) {
	ts, st := setup(t, true)

	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{
		"name": "ur-src", "secret": secret, "allowed_event_types": []string{"push"},
	})
	srcID := int64(src["id"].(float64))

	// 目标主机名为保留的 .invalid 域，DNS 解析必然失败。
	tgt := createResource(t, ts, "/api/v1/targets", map[string]any{
		"name": "ur-tgt", "url": "http://no-such-host.invalid/hook",
	})
	targetID := int64(tgt["id"].(float64))

	// 带 and 分组的条件，且规则目标为多目标列表。
	createResource(t, ts, "/api/v1/rules", map[string]any{
		"name": "ur-rule", "source_id": srcID, "event_type": "push",
		"target_ids": []int64{targetID},
		"condition": []map[string]any{{
			"op": "and",
			"children": []map[string]any{
				{"path": "repo", "op": "eq", "value": "webhook-router"},
			},
		}},
	})

	payload := map[string]any{"repo": "webhook-router"}
	reqBody := map[string]any{
		"source_id": srcID, "event_type": "push", "event_id": "evt-500", "payload": payload,
	}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/events", bytes.NewReader(rawReq))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", sig)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("expected HTTP 500, got transport error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500, got %d", resp.StatusCode)
	}

	// 事件已在投递阶段之前持久化入库。
	ev, err := st.GetByKey(context.Background(), srcID, "evt-500")
	if err != nil {
		t.Fatalf("expected persisted event, got err: %v", err)
	}
	if ev == nil {
		t.Fatal("expected persisted event, got nil")
	}
	if ev.Status != domain.EventAccepted {
		t.Fatalf("expected event status accepted (handler died before update), got %s", ev.Status)
	}
}
