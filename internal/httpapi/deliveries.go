package httpapi

import (
	"net/http"
	"strconv"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
)

func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	eventID, _ := strconv.ParseInt(q.Get("event_id"), 10, 64)
	status := q.Get("status")
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	deliveries, err := s.store.ListDeliveries(r.Context(), eventID, status, page, limit)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "internal error"))
		return
	}
	if deliveries == nil {
		deliveries = []*domain.Delivery{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"deliveries": deliveries, "page": page, "limit": limit})
}
