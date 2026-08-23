package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/engine"
	"webhook-event-router/internal/errs"
	"webhook-event-router/internal/store"
	"webhook-event-router/internal/webhook"
)

type createEventRequest struct {
	SourceID  int64           `json:"source_id"`
	EventType string          `json:"event_type"`
	EventID   string          `json:"event_id"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp int64           `json:"timestamp"`
}

func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxPayload+1))
	if err != nil {
		respondErr(w, errs.New(errs.CodeBadRequest, "failed to read body"))
		return
	}
	var req createEventRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "request is not valid JSON"))
		return
	}
	if req.EventID == "" {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "event_id is required"))
		return
	}
	payloadBytes, _ := json.Marshal(req.Payload)

	src, err := s.store.GetSource(ctx, req.SourceID)
	if err != nil {
		if err == store.ErrNotFound {
			respondErr(w, errs.New(errs.CodeSourceNotFound, "source not found"))
			return
		}
		respondErr(w, errs.New(errs.CodeInternal, "internal error"))
		return
	}
	if !src.Enabled {
		respondErr(w, errs.New(errs.CodeSourceNotFound, "source is disabled"))
		return
	}

	sig := r.Header.Get("X-Signature")
	if !webhook.Verify(src.Secret, sig, body) {
		respondErr(w, errs.New(errs.CodeSignatureInvalid, "signature invalid"))
		return
	}

	if err := webhook.ValidatePayload(payloadBytes, s.cfg.MaxPayload); err != nil {
		respondErr(w, err.(*errs.Error))
		return
	}
	if err := webhook.ValidateEventType(src, req.EventType); err != nil {
		respondErr(w, err.(*errs.Error))
		return
	}

	// 时间戳防重放：事件发生时间必须在允许窗口内。
	var ts *time.Time
	if req.Timestamp != 0 {
		if parsed, ok := webhook.ParseTimestamp(req.Timestamp); ok {
			if err := webhook.ValidateTimestamp(parsed, time.Now().UTC(), s.cfg.ReplayWindow); err != nil {
				respondErr(w, err.(*errs.Error))
				return
			}
			ts = &parsed
		} else {
			respondErr(w, errs.New(errs.CodeReplayRejected, "invalid timestamp"))
			return
		}
	}

	// 幂等去重
	existing, err := s.store.GetByKey(ctx, req.SourceID, req.EventID)
	if err == nil && existing != nil {
		s.recordEvent(domain.EventDuplicate)
		respondJSON(w, http.StatusConflict, map[string]any{
			"code":     "duplicate_event",
			"message":  "duplicate event",
			"event_id": existing.ID,
			"status":   existing.Status,
		})
		return
	}

	event := &domain.Event{
		SourceID:  req.SourceID,
		EventType: req.EventType,
		EventID:   req.EventID,
		Payload:   string(payloadBytes),
		Status:    domain.EventAccepted,
		Timestamp: ts,
	}
	if err := s.store.CreateEvent(ctx, event); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to persist event"))
		return
	}

	// 规则匹配并转发（支持多目标）
	rules, err := s.store.ListEnabledRules(ctx)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to load rules"))
		return
	}
	delivered, failed := s.deliverMatchedTargets(ctx, event, rules, req.SourceID, req.EventType, payloadBytes)

	status := domain.EventDelivered
	if delivered > 0 && failed > 0 {
		status = domain.EventPartiallyFailed
	} else if delivered == 0 && failed > 0 {
		status = domain.EventFailed
	} else if delivered == 0 && failed == 0 {
		status = domain.EventAccepted
	}
	s.store.UpdateStatus(ctx, event.ID, status, "")

	s.recordEvent(domain.EventAccepted)
	respondJSON(w, http.StatusAccepted, map[string]any{
		"event_id": event.ID,
		"status":   "accepted",
	})
}

func (s *Server) deliverMatchedTargets(ctx context.Context, event *domain.Event, rules []*domain.Rule, sourceID int64, eventType string, payload []byte) (int, int) {
	matches := engine.MatchRules(rules, sourceID, eventType, payload)
	delivered := 0
	failed := 0
	seenTarget := map[int64]bool{}
	for _, m := range matches {
		for _, tid := range m.TargetIDs {
			if seenTarget[tid] {
				continue
			}
			target, err := s.store.GetTarget(ctx, tid)
			if err != nil || !target.Enabled {
				continue
			}
			seenTarget[tid] = true
			dv, _ := s.dispatcher.Deliver(ctx, event, m.Rule, target)
			s.recordDeliveryStatus(dv)
			switch dv.Status {
			case domain.DeliveryDelivered:
				delivered++
			default:
				failed++
			}
		}
	}
	return delivered, failed
}

// recordEvent 将事件接入结果计入指标。
func (s *Server) recordEvent(status string) {
	if s.metrics != nil {
		s.metrics.CountEvent(status)
	}
}

// recordDeliveryStatus 将投递结果计入指标。
func (s *Server) recordDeliveryStatus(dv *domain.Delivery) {
	if s.metrics == nil || dv == nil {
		return
	}
	s.metrics.CountDelivery(dv.Status)
}

func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	event, err := s.store.GetEvent(r.Context(), id)
	if err != nil {
		if err == store.ErrNotFound {
			respondErr(w, errs.New(errs.CodeNotFound, "event not found"))
			return
		}
		respondErr(w, errs.New(errs.CodeInternal, "internal error"))
		return
	}
	deliveries, _ := s.store.ListByEvent(r.Context(), event.ID)
	if deliveries == nil {
		deliveries = []*domain.Delivery{}
	}
	// 附带每条投递的尝试明细。
	type deliveryDetail struct {
		Delivery *domain.Delivery          `json:"delivery"`
		Attempts []*domain.DeliveryAttempt `json:"attempts"`
	}
	details := make([]deliveryDetail, 0, len(deliveries))
	for _, dv := range deliveries {
		var atts []*domain.DeliveryAttempt
		if s.attempts != nil {
			atts, _ = s.attempts.ListByDelivery(r.Context(), dv.ID)
		}
		if atts == nil {
			atts = []*domain.DeliveryAttempt{}
		}
		details = append(details, deliveryDetail{Delivery: dv, Attempts: atts})
	}
	respondJSON(w, http.StatusOK, map[string]any{"event": event, "deliveries": details})
}
