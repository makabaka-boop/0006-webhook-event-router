package dispatcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"webhook-event-router/internal/config"
	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/store/sqlite"
)

func setupDispatcher(t *testing.T) (*Dispatcher, *sqlite.Store, *sqlite.AttemptStore, *sqlite.DB) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	st := sqlite.New(db)
	att := sqlite.NewAttemptStore(db)
	cfg := &config.Config{
		MaxRetries:     2,
		RetryBase:      1 * time.Millisecond,
		ForwardTimeout: 2 * time.Second,
		AllowLoopback:  true,
		AllowPrivate:   true,
		ReplayWindow:   300 * time.Second,
	}
	return New(st, att, cfg), st, att, db
}

func TestRequeueResetsAttempts(t *testing.T) {
	d, st, _, _ := setupDispatcher(t)

	// 创建事件、目标、规则与一条 dead 投递。
	src := &domain.Source{Name: "s", Secret: "x", Enabled: true}
	if err := st.CreateSource(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	ev := &domain.Event{SourceID: src.ID, EventType: "push", EventID: "e1", Payload: "{}", Status: domain.EventAccepted}
	if err := st.CreateEvent(context.Background(), ev); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tg := &domain.Target{Name: "t", URL: srv.URL, Enabled: true}
	if err := st.CreateTarget(context.Background(), tg); err != nil {
		t.Fatal(err)
	}
	rule := &domain.Rule{Name: "r", TargetID: tg.ID, Enabled: true}
	if err := st.CreateRule(context.Background(), rule); err != nil {
		t.Fatal(err)
	}

	dv, err := d.Deliver(context.Background(), ev, rule, tg)
	if err != nil {
		t.Fatal(err)
	}
	// 首次失败：attempts 递增并进入 retrying 或 dead。
	if dv.Attempts < 1 {
		t.Fatalf("expected attempts >= 1, got %d", dv.Attempts)
	}

	// 手动标记 dead 再 requeue。
	dv.Status = domain.DeliveryDead
	now := time.Now()
	dv.DeadAt = &now
	dv.Attempts = 5
	if err := st.UpdateDelivery(context.Background(), dv); err != nil {
		t.Fatal(err)
	}

	requeued, err := d.Requeue(context.Background(), dv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.Attempts != 0 {
		t.Fatalf("requeue should reset attempts to 0, got %d", requeued.Attempts)
	}
	if requeued.Status != domain.DeliveryRetrying {
		t.Fatalf("expected retrying after requeue, got %s", requeued.Status)
	}
	if requeued.NextRetryAt == nil {
		t.Fatal("expected next_retry_at after requeue")
	}
	if requeued.DeadAt != nil {
		t.Fatal("dead_at should be cleared after requeue")
	}
}

func TestRequeueRejectsNonDead(t *testing.T) {
	d, st, _, _ := setupDispatcher(t)
	src := &domain.Source{Name: "s", Secret: "x", Enabled: true}
	st.CreateSource(context.Background(), src)
	ev := &domain.Event{SourceID: src.ID, EventType: "p", EventID: "e", Payload: "{}", Status: domain.EventAccepted}
	st.CreateEvent(context.Background(), ev)
	tg := &domain.Target{Name: "t", URL: "http://example.com", Enabled: true}
	st.CreateTarget(context.Background(), tg)
	rule := &domain.Rule{Name: "r", TargetID: tg.ID, Enabled: true}
	st.CreateRule(context.Background(), rule)
	dv := &domain.Delivery{EventID: ev.ID, RuleID: rule.ID, TargetID: tg.ID, Status: domain.DeliveryRetrying}
	st.CreateDelivery(context.Background(), dv)

	if _, err := d.Requeue(context.Background(), dv.ID); err == nil {
		t.Fatal("expected error requeueing non-dead delivery")
	}
}
