package dispatcher

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"webhook-event-router/internal/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type closeSensitiveBody struct {
	reader *strings.Reader
	closed bool
}

func (b *closeSensitiveBody) Read(p []byte) (int, error) {
	if b.closed {
		return 0, errors.New("http: read on closed response body")
	}
	return b.reader.Read(p)
}

func (b *closeSensitiveBody) Close() error {
	b.closed = true
	return nil
}

type recordingAttemptStore struct {
	mu       sync.Mutex
	attempts []domain.DeliveryAttempt
}

func (s *recordingAttemptStore) CreateDeliveryAttempt(_ context.Context, attempt *domain.DeliveryAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, *attempt)
	return nil
}

func (s *recordingAttemptStore) snapshot() []domain.DeliveryAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.DeliveryAttempt(nil), s.attempts...)
}

func TestDeliverAcceptedResponseIsRecordedWithoutRetry(t *testing.T) {
	d, st, _, _ := setupDispatcher(t)
	recorded := &recordingAttemptStore{}
	d.attempts = recorded

	const responseBody = `{"accepted":true}`
	var received atomic.Int32
	var receivedMu sync.Mutex
	var receivedBodies []string
	d.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		received.Add(1)
		requestBody, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		receivedMu.Lock()
		receivedBodies = append(receivedBodies, string(requestBody))
		receivedMu.Unlock()
		return &http.Response{
			StatusCode:    http.StatusAccepted,
			Status:        "202 Accepted",
			Header:        make(http.Header),
			Body:          &closeSensitiveBody{reader: strings.NewReader(responseBody)},
			ContentLength: int64(len(responseBody)),
			Request:       request,
		}, nil
	})}

	ctx := context.Background()
	source := &domain.Source{Name: "accepted-source", Secret: "secret", Enabled: true}
	if err := st.CreateSource(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}
	target := &domain.Target{Name: "accepted-target", URL: "http://127.0.0.1/webhooks", Enabled: true}
	if err := st.CreateTarget(ctx, target); err != nil {
		t.Fatalf("create target: %v", err)
	}
	rule := &domain.Rule{Name: "accepted-rule", TargetID: target.ID, Enabled: true}
	if err := st.CreateRule(ctx, rule); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	event := &domain.Event{
		SourceID:  source.ID,
		EventType: "push",
		EventID:   "accepted-event",
		Payload:   `{"ref":"main"}`,
		Status:    domain.EventAccepted,
	}
	if err := st.CreateEvent(ctx, event); err != nil {
		t.Fatalf("create event: %v", err)
	}

	delivery, err := d.Deliver(ctx, event, rule, target)
	if err != nil {
		t.Fatalf("deliver event: %v", err)
	}
	if got := received.Load(); got != 1 {
		t.Errorf("receiver request count after initial delivery = %d, want 1", got)
	}
	receivedMu.Lock()
	if len(receivedBodies) != 1 || receivedBodies[0] != event.Payload {
		t.Errorf("receiver request bodies = %q, want [%q]", receivedBodies, event.Payload)
	}
	receivedMu.Unlock()

	details, err := st.ListByEvent(ctx, event.ID)
	if err != nil {
		t.Fatalf("query event delivery details: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("event delivery count = %d, want 1", len(details))
	}
	gotDelivery := details[0]
	if gotDelivery.ID != delivery.ID {
		t.Errorf("queried delivery ID = %d, want %d", gotDelivery.ID, delivery.ID)
	}
	if gotDelivery.Status != domain.DeliveryDelivered {
		t.Errorf("delivery status = %q, want %q", gotDelivery.Status, domain.DeliveryDelivered)
	}
	if gotDelivery.LastError != "" {
		t.Errorf("delivery last error = %q, want empty", gotDelivery.LastError)
	}
	if gotDelivery.NextRetryAt != nil {
		t.Errorf("delivery next retry = %v, want nil", gotDelivery.NextRetryAt)
	}
	if gotDelivery.Attempts != 1 {
		t.Errorf("delivery attempts = %d, want 1", gotDelivery.Attempts)
	}

	attempts := recorded.snapshot()
	if len(attempts) != 1 {
		t.Errorf("recorded attempt count after initial delivery = %d, want 1", len(attempts))
	} else {
		if attempts[0].ResponseStatus != http.StatusAccepted {
			t.Errorf("recorded response status = %d, want %d", attempts[0].ResponseStatus, http.StatusAccepted)
		}
		if attempts[0].ResponseBody != responseBody {
			t.Errorf("recorded response body = %q, want %q", attempts[0].ResponseBody, responseBody)
		}
		if attempts[0].Error != "" {
			t.Errorf("recorded attempt error = %q, want empty", attempts[0].Error)
		}
		if attempts[0].Status != domain.DeliverySent {
			t.Errorf("recorded attempt status = %q, want %q", attempts[0].Status, domain.DeliverySent)
		}
	}

	if gotDelivery.Status == domain.DeliveryRetrying && gotDelivery.NextRetryAt != nil {
		past := time.Unix(0, 0)
		gotDelivery.NextRetryAt = &past
		if err := st.UpdateDelivery(ctx, gotDelivery); err != nil {
			t.Fatalf("make retry due: %v", err)
		}
	}
	d.Retry(ctx)
	if got := received.Load(); got != 1 {
		t.Errorf("receiver request count after retry processing = %d, want 1", got)
	}
	if got := len(recorded.snapshot()); got != 1 {
		t.Errorf("recorded attempt count after retry processing = %d, want 1", got)
	}
	finalDelivery, err := st.GetDelivery(ctx, delivery.ID)
	if err != nil {
		t.Fatalf("query delivery after retry processing: %v", err)
	}
	if finalDelivery.Status != domain.DeliveryDelivered {
		t.Errorf("delivery status after retry processing = %q, want %q", finalDelivery.Status, domain.DeliveryDelivered)
	}
	if finalDelivery.Attempts != 1 {
		t.Errorf("delivery attempts after retry processing = %d, want 1", finalDelivery.Attempts)
	}
	if finalDelivery.LastError != "" {
		t.Errorf("delivery last error after retry processing = %q, want empty", finalDelivery.LastError)
	}
}
