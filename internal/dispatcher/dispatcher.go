package dispatcher

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"webhook-event-router/internal/config"
	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/store"
)

// Dispatcher 负责转发与后台重试。
type Dispatcher struct {
	store         store.Store
	attempts      AttemptStore
	client        *http.Client
	cfg           *config.Config
	allowPrivate  bool
	allowLoopback bool
	backoff       *Backoff
}

// AttemptStore 定义投递尝试写入依赖。
type AttemptStore interface {
	CreateDeliveryAttempt(ctx context.Context, a *domain.DeliveryAttempt) error
}

// New 构造 Dispatcher。
func New(st store.Store, att AttemptStore, cfg *config.Config) *Dispatcher {
	client := &http.Client{Timeout: cfg.ForwardTimeout}
	return &Dispatcher{
		store:         st,
		attempts:      att,
		client:        client,
		cfg:           cfg,
		allowPrivate:  cfg.AllowPrivate,
		allowLoopback: cfg.AllowLoopback,
		backoff:       NewBackoff(cfg.RetryBase),
	}
}

// Deliver 对单个目标执行转发并记录投递与尝试。
func (d *Dispatcher) Deliver(ctx context.Context, event *domain.Event, rule *domain.Rule, target *domain.Target) (*domain.Delivery, error) {
	delivery := &domain.Delivery{
		EventID:  event.ID,
		RuleID:   rule.ID,
		TargetID: target.ID,
		Status:   domain.DeliveryPending,
	}
	if err := d.store.CreateDelivery(ctx, delivery); err != nil {
		return nil, err
	}
	if !Allowed(target.URL, d.allowPrivate, d.allowLoopback) {
		delivery.Status = domain.DeliveryDead
		delivery.LastError = "target not whitelisted"
		now := time.Now()
		delivery.DeadAt = &now
		d.store.UpdateDelivery(ctx, delivery)
		return delivery, nil
	}
	d.send(ctx, delivery, target, event.Payload)
	return delivery, nil
}

func (d *Dispatcher) send(ctx context.Context, delivery *domain.Delivery, target *domain.Target, body string) {
	att := &domain.DeliveryAttempt{DeliveryID: delivery.ID, RequestBody: body, StartedAt: time.Now()}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewBufferString(body))
	if err != nil {
		att.Status = domain.DeliveryFailed
		att.Error = err.Error()
		att.FinishedAt = time.Now()
		d.attempts.CreateDeliveryAttempt(ctx, att)
		d.markFailed(delivery, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		att.Status = domain.DeliveryFailed
		att.Error = err.Error()
		att.FinishedAt = time.Now()
		d.attempts.CreateDeliveryAttempt(ctx, att)
		d.markFailed(delivery, err.Error())
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	att.Status = domain.DeliverySent
	att.ResponseStatus = resp.StatusCode
	att.ResponseBody = string(respBody)
	att.FinishedAt = time.Now()
	d.attempts.CreateDeliveryAttempt(ctx, att)

	delivery.Attempts++
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		delivery.Status = domain.DeliveryDelivered
		delivery.LastError = ""
		d.store.UpdateDelivery(ctx, delivery)
		return
	}
	d.markFailed(delivery, "unexpected status "+http.StatusText(resp.StatusCode))
}

func (d *Dispatcher) markFailed(delivery *domain.Delivery, reason string) {
	delivery.Attempts++
	delivery.LastError = reason
	if delivery.Attempts >= d.cfg.MaxRetries {
		delivery.Status = domain.DeliveryDead
		delivery.NextRetryAt = nil
		now := time.Now()
		delivery.DeadAt = &now
	} else {
		delivery.Status = domain.DeliveryRetrying
		next := time.Now().Add(d.backoff.Duration(delivery.Attempts))
		delivery.NextRetryAt = &next
	}
	d.store.UpdateDelivery(ctxWithoutCancel(), delivery)
}

// Retry 处理到期的投递（后台调用）。
func (d *Dispatcher) Retry(ctx context.Context) {
	nowUnix := time.Now().Unix()
	due, err := d.store.DueDeliveries(ctx, nowUnix, 100)
	if err != nil || len(due) == 0 {
		return
	}
	for _, dv := range due {
		event, err := d.store.GetEvent(ctx, dv.EventID)
		if err != nil {
			continue
		}
		target, err := d.store.GetTarget(ctx, dv.TargetID)
		if err != nil {
			continue
		}
		d.send(ctx, dv, target, event.Payload)
	}
}

func ctxWithoutCancel() context.Context {
	return context.Background()
}
