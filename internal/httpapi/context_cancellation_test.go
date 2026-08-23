package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/webhook"
)

// TestCanceledEventRequestKeepsDeliveryFailure verifies that canceling the
// client request does not discard the delivery outcome or its retry record.
func TestCanceledEventRequestKeepsDeliveryFailure(t *testing.T) {
	var receivedOnce sync.Once
	received := make(chan struct{})
	done := make(chan struct{})
	var callsMu sync.Mutex
	calls := 0

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		receivedOnce.Do(func() { close(received) })
		<-r.Context().Done()
		close(done)
	}))
	defer target.Close()

	ts, st := setup(t, true)
	secret := "cancel-secret"
	src := createResource(t, ts, "/api/v1/sources", map[string]any{
		"name": "cancel-source", "secret": secret, "allowed_event_types": []string{"push"},
	})
	sourceID := int64(src["id"].(float64))
	tg := createResource(t, ts, "/api/v1/targets", map[string]any{
		"name": "blocking-target", "url": target.URL,
	})
	targetID := int64(tg["id"].(float64))
	createResource(t, ts, "/api/v1/rules", map[string]any{
		"name": "cancel-rule", "source_id": sourceID, "event_type": "push", "target_id": targetID,
	})

	requestBody := map[string]any{
		"source_id": sourceID, "event_type": "push", "event_id": "cancel-event-unique",
		"payload": map[string]any{"repo": "router"},
	}
	rawBody, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+"/api/v1/events", bytes.NewReader(rawBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Signature", webhook.Sign(secret, rawBody))
	responseCh := make(chan struct {
		resp *http.Response
		err  error
	}, 1)
	go func() {
		resp, err := ts.Client().Do(req)
		responseCh <- struct {
			resp *http.Response
			err  error
		}{resp: resp, err: err}
	}()

	select {
	case <-received:
		cancel()
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("downstream did not receive the event")
	}

	// The canceled client must not observe a complete accepted response.
	result := <-responseCh
	completeResponse := false
	if result.err == nil && result.resp != nil {
		body, _ := io.ReadAll(result.resp.Body)
		result.resp.Body.Close()
		var envelope map[string]any
		if json.Unmarshal(body, &envelope) == nil && result.resp.StatusCode == http.StatusAccepted {
			_, hasEventID := envelope["event_id"]
			_, hasStatus := envelope["status"]
			completeResponse = hasEventID && hasStatus
		}
	}
	if completeResponse {
		t.Fatal("canceled request unexpectedly returned a complete response")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("downstream request did not terminate after cancellation")
	}

	event, err := st.GetByKey(context.Background(), sourceID, "cancel-event-unique")
	if err != nil {
		t.Fatalf("event was not persisted: %v", err)
	}
	if event.Status != domain.EventFailed {
		t.Fatalf("expected failed event status after canceled delivery, got %q", event.Status)
	}

	// Read the public event-details route so delivery and attempt records are
	// checked through the same boundary used by clients.
	resp, details := doJSON(t, ts.Client(), http.MethodGet,
		ts.URL+"/api/v1/events/"+strconv.FormatInt(event.ID, 10), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("event details returned %d: %v", resp.StatusCode, details)
	}
	deliveries, ok := details["deliveries"].([]any)
	if !ok || len(deliveries) != 1 {
		t.Fatalf("expected one delivery detail, got %v", details["deliveries"])
	}
	detail, ok := deliveries[0].(map[string]any)
	if !ok {
		t.Fatalf("invalid delivery detail: %T", deliveries[0])
	}
	delivery, ok := detail["delivery"].(map[string]any)
	if !ok {
		t.Fatalf("missing delivery record: %v", detail)
	}
	if delivery["status"] != string(domain.DeliveryRetrying) {
		t.Fatalf("expected retrying delivery, got %v", delivery["status"])
	}
	if attempts, _ := delivery["attempts"].(float64); attempts < 1 {
		t.Fatalf("expected at least one failed attempt, got %v", delivery["attempts"])
	}
	attempts, ok := detail["attempts"].([]any)
	if !ok || len(attempts) < 1 {
		t.Fatalf("expected failed attempt details, got %v", detail["attempts"])
	}
	firstAttempt, ok := attempts[0].(map[string]any)
	if !ok || firstAttempt["status"] != string(domain.DeliveryFailed) || firstAttempt["error"] == "" {
		t.Fatalf("expected failed attempt with error, got %v", attempts[0])
	}

	due, err := st.DueDeliveries(context.Background(), time.Now().Add(24*time.Hour).Unix(), 100)
	if err != nil {
		t.Fatalf("due delivery query failed: %v", err)
	}
	found := false
	for _, d := range due {
		if d.ID == event.ID || d.EventID == event.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("canceled delivery was not eligible for a later retry: %+v", due)
	}

	callsMu.Lock()
	if calls != 1 {
		t.Fatalf("expected one downstream request before retry, got %d", calls)
	}
	callsMu.Unlock()
}
