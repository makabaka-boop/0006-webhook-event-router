package dispatcher_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"webhook-event-router/internal/config"
	"webhook-event-router/internal/dispatcher"
	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/httpapi"
	"webhook-event-router/internal/metrics"
	"webhook-event-router/internal/store/sqlite"
	"webhook-event-router/internal/webhook"
)

type cancellationTransport struct {
	entered chan struct{}
	calls   atomic.Int32
}

type canceledResponseWriter struct {
	header   http.Header
	code     int
	writeErr error
}

func (w *canceledResponseWriter) Header() http.Header  { return w.header }
func (w *canceledResponseWriter) WriteHeader(code int) { w.code = code }
func (w *canceledResponseWriter) Write([]byte) (int, error) {
	w.writeErr = context.Canceled
	return 0, context.Canceled
}

func (t *cancellationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	call := t.calls.Add(1)
	if call == 1 {
		close(t.entered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return nil, errors.New("retry transport failure")
}

func TestCanceledEventRequestPersistsAttemptAndRetry(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	st := sqlite.New(db)
	att := sqlite.NewAttemptStore(db)
	cfg := &config.Config{
		Addr:           ":0",
		MaxPayload:     1 << 20,
		MaxRetries:     5,
		RetryBase:      time.Hour,
		ForwardTimeout: 5 * time.Second,
		AllowLoopback:  true,
		AllowPrivate:   true,
		ReplayWindow:   5 * time.Minute,
	}
	disp := dispatcher.New(st, att, cfg)
	transport := &cancellationTransport{entered: make(chan struct{})}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	handler := httpapi.NewServer(st, cfg, disp, metrics.NewRegistry(), att)

	source := callJSON(t, handler, http.MethodPost, "/api/v1/sources", map[string]any{
		"name": "cancel-source", "secret": "cancel-secret",
	})
	sourceID := int64(source["id"].(float64))
	target := callJSON(t, handler, http.MethodPost, "/api/v1/targets", map[string]any{
		"name": "blocked-target", "url": "http://example.com/hook",
	})
	targetID := int64(target["id"].(float64))
	callJSON(t, handler, http.MethodPost, "/api/v1/rules", map[string]any{
		"name": "cancel-rule", "source_id": sourceID, "target_id": targetID,
	})

	eventBody := map[string]any{
		"source_id": sourceID, "event_type": "push", "event_id": "cancel-event",
		"payload": map[string]any{"value": "blocked"},
	}
	rawBody, err := json.Marshal(eventBody)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(rawBody)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", webhook.Sign("cancel-secret", rawBody))
	result := &canceledResponseWriter{header: make(http.Header)}
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(result, req)
		close(done)
	}()
	select {
	case <-transport.entered:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("delivery did not start")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event handler did not return after cancellation")
	}
	if result.code != http.StatusAccepted {
		t.Fatalf("expected accepted response status before client cancellation, got %d", result.code)
	}
	if !errors.Is(result.writeErr, context.Canceled) {
		t.Fatalf("expected client response to be canceled before a complete body, got %v", result.writeErr)
	}

	event, err := st.GetByKey(context.Background(), sourceID, "cancel-event")
	if err != nil || event == nil {
		t.Fatalf("load persisted event: %v", err)
	}
	eventID := event.ID
	query := callJSON(t, handler, http.MethodGet, "/api/v1/events/"+jsonNumber(eventID), nil)
	if query["event"].(map[string]any)["status"] != domain.EventAccepted {
		t.Fatalf("expected event to remain accepted, got %v", query["event"].(map[string]any)["status"])
	}
	details := query["deliveries"].([]any)
	if len(details) != 1 {
		t.Fatalf("expected one delivery, got %d", len(details))
	}
	delivery := details[0].(map[string]any)["delivery"].(map[string]any)
	if delivery["status"] != domain.DeliveryRetrying && delivery["status"] != domain.DeliveryDead {
		t.Fatalf("expected retrying or dead delivery after cancellation, got %v", delivery["status"])
	}
	if delivery["attempts"].(float64) < 1 {
		t.Fatalf("expected a recorded failed attempt, got %v", delivery["attempts"])
	}
	attempts := details[0].(map[string]any)["attempts"].([]any)
	if len(attempts) < 1 {
		t.Fatal("expected cancellation attempt details to be persisted")
	}
	if attempts[0].(map[string]any)["status"] != domain.DeliveryFailed {
		t.Fatalf("expected failed attempt status, got %v", attempts[0].(map[string]any)["status"])
	}

	dels, err := st.ListDeliveries(context.Background(), eventID, "", 1, 10)
	if err != nil || len(dels) != 1 {
		t.Fatalf("load delivery: %v (count %d)", err, len(dels))
	}
	past := time.Now().Add(-time.Minute)
	dels[0].NextRetryAt = &past
	if err := st.UpdateDelivery(context.Background(), dels[0]); err != nil {
		t.Fatalf("make delivery due: %v", err)
	}
	disp.Retry(context.Background())
	if got := transport.calls.Load(); got != 2 {
		t.Fatalf("expected retry scan to issue a second request, got %d calls", got)
	}
	finalAttempts, err := att.ListByDelivery(context.Background(), dels[0].ID)
	if err != nil {
		t.Fatalf("list retry attempts: %v", err)
	}
	if len(finalAttempts) < 2 {
		t.Fatalf("expected retry attempt detail, got %d attempts", len(finalAttempts))
	}
	updated, err := st.GetDelivery(context.Background(), dels[0].ID)
	if err != nil {
		t.Fatalf("load delivery after retry: %v", err)
	}
	if updated.Attempts < 2 {
		t.Fatalf("expected delivery attempts to advance after retry, got %d", updated.Attempts)
	}
	if updated.Status != domain.DeliveryRetrying && updated.Status != domain.DeliveryDead {
		t.Fatalf("expected retrying or dead delivery after retry failure, got %s", updated.Status)
	}
}

func callJSON(t *testing.T, handler http.Handler, method, path string, body any) map[string]any {
	t.Helper()
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code < 200 || res.Code >= 300 {
		t.Fatalf("%s %s got %d: %s", method, path, res.Code, res.Body.String())
	}
	var out map[string]any
	if res.Body.Len() > 0 {
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s response: %v", path, err)
		}
	}
	return out
}

func jsonNumber(id int64) string {
	return strconv.FormatInt(id, 10)
}
