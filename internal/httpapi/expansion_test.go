package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"webhook-event-router/internal/webhook"
)

func TestAuditQuery(t *testing.T) {
	ts, st := setup(t, true)
	// 创建来源产生一条 change log
	createResource(t, ts, "/api/v1/sources", map[string]any{"name": "audit-src", "secret": "s"})
	createResource(t, ts, "/api/v1/targets", map[string]any{"name": "audit-tgt", "url": "http://localhost:9/hook"})

	resp, m := doJSON(t, ts.Client(), http.MethodGet, ts.URL+"/api/v1/audit?entity_type=source", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %v", resp.StatusCode, m)
	}
	logs, ok := m["audit_logs"].([]any)
	if !ok || len(logs) == 0 {
		t.Fatalf("expected at least one audit log, got %v", m["audit_logs"])
	}
	_ = st
}

func TestMetricsEndpoint(t *testing.T) {
	ts, st := setup(t, true)
	_ = st
	// 触发一次事件接入，使计数器有值。
	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "m-src", "secret": secret})
	srcID := int64(src["id"].(float64))
	reqBody := map[string]any{"source_id": srcID, "event_type": "x", "event_id": "m1", "payload": map[string]any{}}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)
	doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})

	resp, body := doRaw(t, ts.Client(), ts.URL+"/metrics")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty metrics body")
	}
}

func TestManualRetryEndpoint(t *testing.T) {
	targetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer targetSrv.Close()

	ts, st := setup(t, true)
	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "r-src", "secret": secret})
	srcID := int64(src["id"].(float64))
	tg := createResource(t, ts, "/api/v1/targets", map[string]any{"name": "r-tgt", "url": targetSrv.URL})
	targetID := int64(tg["id"].(float64))
	createResource(t, ts, "/api/v1/rules", map[string]any{"name": "r", "source_id": srcID, "target_id": targetID})

	reqBody := map[string]any{"source_id": srcID, "event_type": "x", "event_id": "r1", "payload": map[string]any{}}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)
	resp, _ := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	dels, _ := st.ListDeliveries(context.Background(), 0, "", 1, 100)
	if len(dels) == 0 {
		t.Fatal("expected a delivery")
	}
	dv := dels[0]
	// 手动将投递置为 dead。
	dv.Status = "dead"
	dv.Attempts = 5
	now := time.Now()
	dv.DeadAt = &now
	st.UpdateDelivery(context.Background(), dv)

	resp2, m2 := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/deliveries/"+strconv.FormatInt(dv.ID, 10)+"/retry", nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 retry, got %d: %v", resp2.StatusCode, m2)
	}
	if int(m2["attempts"].(float64)) != 0 {
		t.Fatalf("expected attempts reset to 0, got %v", m2["attempts"])
	}
	if m2["status"] != "retrying" {
		t.Fatalf("expected retrying, got %v", m2["status"])
	}
}

func TestTimestampReplayRejected(t *testing.T) {
	ts, _ := setup(t, true)
	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "replay-src", "secret": secret})
	srcID := int64(src["id"].(float64))
	// 时间戳过旧，应被防重放拒绝。
	stale := time.Now().Add(-10 * time.Minute).Unix()
	reqBody := map[string]any{"source_id": srcID, "event_type": "x", "event_id": "e", "payload": map[string]any{}, "timestamp": stale}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)
	resp, m := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %v", resp.StatusCode, m)
	}
	if m["code"] != "replay_rejected" {
		t.Fatalf("expected replay_rejected, got %v", m["code"])
	}
}

func TestMultiTargetRule(t *testing.T) {
	ts, st := setup(t, true)

	hits1 := 0
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits1++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv1.Close()
	hits2 := 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits2++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()

	secret := "s"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{"name": "mt-src", "secret": secret})
	srcID := int64(src["id"].(float64))
	t1 := createResource(t, ts, "/api/v1/targets", map[string]any{"name": "mt-1", "url": srv1.URL})
	t2 := createResource(t, ts, "/api/v1/targets", map[string]any{"name": "mt-2", "url": srv2.URL})
	t1ID := int64(t1["id"].(float64))
	t2ID := int64(t2["id"].(float64))

	createResource(t, ts, "/api/v1/rules", map[string]any{
		"name": "mt-rule", "source_id": srcID, "target_ids": []int64{t1ID, t2ID},
	})

	reqBody := map[string]any{"source_id": srcID, "event_type": "x", "event_id": "mt1", "payload": map[string]any{}}
	rawReq, _ := json.Marshal(reqBody)
	sig := webhook.Sign(secret, rawReq)
	resp, _ := doJSON(t, ts.Client(), http.MethodPost, ts.URL+"/api/v1/events", reqBody, map[string]string{"X-Signature": sig})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if hits1 != 1 || hits2 != 1 {
		t.Fatalf("expected both targets hit once, got %d and %d", hits1, hits2)
	}
	_ = st
}

func doRaw(t *testing.T, client *http.Client, url string) (*http.Response, string) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf, _ := io.ReadAll(resp.Body)
	return resp, string(buf)
}
