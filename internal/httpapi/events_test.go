package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"webhook-event-router/internal/config"
	"webhook-event-router/internal/dispatcher"
	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/metrics"
	"webhook-event-router/internal/store/sqlite"
	"webhook-event-router/internal/webhook"
)

type recordingTransport struct {
	mu       sync.Mutex
	requests [][]byte
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	rt.mu.Lock()
	rt.requests = append(rt.requests, body)
	rt.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Status:     "204 No Content",
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Request:    req,
	}, nil
}

func (rt *recordingTransport) count() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.requests)
}

func setupDirectHandler(t *testing.T) (http.Handler, *sqlite.Store, *recordingTransport) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "wildcard-event.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	st := sqlite.New(db)
	attempts := sqlite.NewAttemptStore(db)
	cfg := &config.Config{
		MaxPayload:     1 << 20,
		MaxRetries:     1,
		RetryBase:      time.Millisecond,
		ForwardTimeout: time.Second,
		AllowLoopback:  true,
		AllowPrivate:   true,
		ReplayWindow:   300 * time.Second,
	}
	recorder := &recordingTransport{}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = recorder
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	disp := dispatcher.New(st, attempts, cfg)
	return NewServer(st, cfg, disp, metrics.NewRegistry(), attempts), st, recorder
}

func directJSON(t *testing.T, handler http.Handler, method, path string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var response map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response for %s: %v", path, err)
		}
	}
	return w.Code, response
}

func createDirectResource(t *testing.T, handler http.Handler, path string, body map[string]any) map[string]any {
	t.Helper()
	status, response := directJSON(t, handler, http.MethodPost, path, body, nil)
	if status != http.StatusCreated {
		t.Fatalf("create %s got %d: %v", path, status, response)
	}
	return response
}

func TestCreateEventWildcardRuleDeliversAndKeepsIndexedRuleBehavior(t *testing.T) {
	handler, st, target := setupDirectHandler(t)
	secret := "wildcard-secret"
	source := createDirectResource(t, handler, "/api/v1/sources", map[string]any{
		"name":                "wildcard-source",
		"secret":              secret,
		"enabled":             true,
		"allowed_event_types": []string{"wildcard-alert", "indexed-alert"},
	})
	sourceID := int64(source["id"].(float64))
	targetResource := createDirectResource(t, handler, "/api/v1/targets", map[string]any{
		"name":    "recording-target",
		"url":     "http://127.0.0.1/webhook",
		"enabled": true,
	})
	targetID := int64(targetResource["id"].(float64))

	for _, rule := range []struct {
		name      string
		eventType string
		path      string
	}{
		{name: "wildcard-critical", eventType: "wildcard-alert", path: "items.*.severity"},
		{name: "indexed-critical", eventType: "indexed-alert", path: "items.0.severity"},
	} {
		createDirectResource(t, handler, "/api/v1/rules", map[string]any{
			"name":       rule.name,
			"source_id":  sourceID,
			"event_type": rule.eventType,
			"target_id":  targetID,
			"enabled":    true,
			"condition": []map[string]any{{
				"path": rule.path, "op": "eq", "value": "critical",
			}},
		})
	}

	for _, scenario := range []struct {
		name      string
		eventType string
		eventID   string
	}{
		{name: "wildcard path", eventType: "wildcard-alert", eventID: "evt-wildcard-critical"},
		{name: "indexed path", eventType: "indexed-alert", eventID: "evt-indexed-critical"},
	} {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			requestsBefore := target.count()
			requestBody := map[string]any{
				"source_id":  sourceID,
				"event_type": scenario.eventType,
				"event_id":   scenario.eventID,
				"payload": map[string]any{
					"items": []map[string]any{{"severity": "critical"}},
				},
			}
			rawRequest, err := json.Marshal(requestBody)
			if err != nil {
				t.Fatalf("marshal signed event: %v", err)
			}
			status, response := directJSON(t, handler, http.MethodPost, "/api/v1/events", requestBody, map[string]string{
				"X-Signature": webhook.Sign(secret, rawRequest),
			})
			if status != http.StatusAccepted {
				t.Errorf("expected event endpoint to return 202, got %d: %v", status, response)
			}
			if response["status"] != "accepted" {
				t.Errorf("expected response status accepted, got %v", response["status"])
			}

			event, err := st.GetByKey(context.Background(), sourceID, scenario.eventID)
			if err != nil {
				t.Fatalf("query persisted event %q: %v", scenario.eventID, err)
			}
			if event.Status != domain.EventDelivered {
				t.Errorf("expected persisted event %q to be delivered, got %s", scenario.eventID, event.Status)
			}
			deliveries, err := st.ListByEvent(context.Background(), event.ID)
			if err != nil {
				t.Fatalf("query deliveries for %q: %v", scenario.eventID, err)
			}
			if len(deliveries) != 1 {
				t.Errorf("expected one delivery for %q, got %d", scenario.eventID, len(deliveries))
			} else if deliveries[0].Status != domain.DeliveryDelivered {
				t.Errorf("expected delivery for %q to be delivered, got %s", scenario.eventID, deliveries[0].Status)
			}
			if got := target.count() - requestsBefore; got != 1 {
				t.Fatalf("expected target to receive one request for %q, got %d", scenario.eventID, got)
			}
		})
	}
}

// createSource 创建一个测试来源，返回其 ID 与密钥。
func createResource(t *testing.T, ts *httptest.Server, path string, body map[string]any) map[string]any {
	t.Helper()
	resp, m := doJSON(t, ts.Client(), http.MethodPost, ts.URL+path, body, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create %s got %d: %v", path, resp.StatusCode, m)
	}
	return m
}

func TestEndToEndFlow(t *testing.T) {
	ts, _ := setup(t, true)

	secret := "test-secret"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{
		"name": "github", "secret": secret, "allowed_event_types": []string{"push"},
	})
	srcID := int64(src["id"].(float64))

	targets := createResource(t, ts, "/api/v1/targets", map[string]any{
		"name": "internal", "url": "http://localhost:9999/hook",
	})
	targetID := int64(targets["id"].(float64))

	createResource(t, ts, "/api/v1/rules", map[string]any{
		"name": "route-push", "source_id": srcID, "event_type": "push",
		"target_id": targetID,
		"condition": []map[string]any{{"path": "repo", "op": "eq", "value": "webhook-router"}},
	})

	payload := map[string]any{"repo": "webhook-router", "ref": "refs/heads/main"}
	rawPayload, _ := json.Marshal(payload)
	reqBody := map[string]any{
		"source_id": srcID, "event_type": "push", "event_id": "evt-1", "payload": payload,
	}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)

	// 首次接入
	resp, m := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", resp.StatusCode, m)
	}
	if m["status"] != "accepted" {
		t.Fatalf("expected accepted, got %v", m["status"])
	}

	// 幂等：第二次同 (source_id, event_id) 返回 409 duplicate
	resp2, m2 := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 duplicate, got %d: %v", resp2.StatusCode, m2)
	}
	if m2["code"] != "duplicate_event" {
		t.Fatalf("expected duplicate_event, got %v", m2["code"])
	}

	_ = rawPayload
}

func TestInvalidPayloadRejected(t *testing.T) {
	ts, _ := setup(t, true)
	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "s1", "secret": secret})
	srcID := int64(src["id"].(float64))
	rawReq, _ := json.Marshal(map[string]any{"source_id": srcID, "event_type": "x", "event_id": "e", "payload": "not-a-json"})
	sig := webhook.Sign(secret, rawReq)
	resp, m := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events",
		map[string]any{"source_id": srcID, "event_type": "x", "event_id": "e", "payload": "not-a-json"},
		map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", resp.StatusCode, m)
	}
	if m["code"] != "payload_invalid" {
		t.Fatalf("expected payload_invalid, got %v", m["code"])
	}
}

func TestUnknownEventTypeRejected(t *testing.T) {
	ts, _ := setup(t, true)
	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "s2", "secret": secret, "allowed_event_types": []string{"push"}})
	srcID := int64(src["id"].(float64))
	reqBody := map[string]any{"source_id": srcID, "event_type": "issue", "event_id": "e", "payload": map[string]any{}}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)
	resp, m := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %v", resp.StatusCode, m)
	}
	if m["code"] != "event_type_not_allowed" {
		t.Fatalf("expected event_type_not_allowed, got %v", m["code"])
	}
}

func TestBadSignatureRejected(t *testing.T) {
	ts, _ := setup(t, true)
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "s3", "secret": "real"})
	srcID := int64(src["id"].(float64))
	reqBody := map[string]any{"source_id": srcID, "event_type": "x", "event_id": "e", "payload": map[string]any{}}
	resp, m := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": "wrong"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %v", resp.StatusCode, m)
	}
	if m["code"] != "signature_invalid" {
		t.Fatalf("expected signature_invalid, got %v", m["code"])
	}
}

func TestHealthz(t *testing.T) {
	ts, _ := setup(t, true)
	resp, m := doJSON(t, ts.Client(), http.MethodGet, ts.URL+"/healthz", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if m["status"] != "ok" {
		t.Fatalf("expected ok, got %v", m["status"])
	}
}

func TestDeliveryRetry(t *testing.T) {
	// 目标返回 500，触发重试最终 dead
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer targetSrv.Close()

	ts, st := setup(t, true)
	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "s4", "secret": secret})
	srcID := int64(src["id"].(float64))
	tg := createResource(t, ts, "/api/v1/targets", map[string]any{"name": "t", "url": targetSrv.URL})
	targetID := int64(tg["id"].(float64))
	createResource(t, ts, "/api/v1/rules", map[string]any{
		"name": "r", "source_id": srcID, "target_id": targetID,
	})

	reqBody := map[string]any{"source_id": srcID, "event_type": "x", "event_id": "e", "payload": map[string]any{"a": "b"}}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)
	resp, _ := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	// 查询投递，验证失败留痕与重试状态
	dels, _ := st.ListDeliveries(context.Background(), 0, "", 1, 100)
	if len(dels) == 0 {
		t.Fatal("expected at least one delivery")
	}
	if dels[0].Attempts < 1 {
		t.Fatalf("expected attempts >= 1, got %d", dels[0].Attempts)
	}
	if dels[0].Status != "retrying" && dels[0].Status != "dead" {
		t.Fatalf("expected retrying/dead, got %s", dels[0].Status)
	}
}

func TestWhitelistBlocked(t *testing.T) {
	ts, _ := setup(t, false) // 不允许环回
	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "s5", "secret": secret})
	srcID := int64(src["id"].(float64))
	createResource(t, ts, "/api/v1/targets", map[string]any{"name": "loopback", "url": "http://127.0.0.1:9/hook"})
	createResource(t, ts, "/api/v1/rules", map[string]any{"name": "r", "source_id": srcID, "target_id": 1})

	reqBody := map[string]any{"source_id": srcID, "event_type": "x", "event_id": "e", "payload": map[string]any{}}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)
	resp, _ := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 (delivery-level failure), got %d", resp.StatusCode)
	}
}
