package dispatcher

import (
	"context"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
)

// Requeue 将一条 dead 投递重新放入重试队列并复位尝试计数，
// 使其按照完整退避周期重新重试。仅 dead 状态允许手动重试。
func (d *Dispatcher) Requeue(ctx context.Context, deliveryID int64) (*domain.Delivery, error) {
	dv, err := d.store.GetDelivery(ctx, deliveryID)
	if err != nil {
		return nil, err
	}
	if dv.Status != domain.DeliveryDead {
		return nil, errs.New(errs.CodeNotRetryable, "only dead deliveries can be retried manually")
	}
	dv.Attempts = 0
	dv.Status = domain.DeliveryRetrying
	dv.LastError = ""
	dv.DeadAt = nil
	next := time.Now().Add(d.backoff.Duration(1))
	dv.NextRetryAt = &next
	if err := d.store.UpdateDelivery(ctx, dv); err != nil {
		return nil, err
	}
	return dv, nil
}
