package httpapi

import (
	"net/http"
	"strconv"

	"webhook-event-router/internal/errs"
	"webhook-event-router/internal/store"
)

// handleRetryDelivery 手动重试一条 dead 投递。
// POST /api/v1/deliveries/{id}/retry
func (s *Server) handleRetryDelivery(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	dv, err := s.dispatcher.Requeue(r.Context(), id)
	if err != nil {
		switch {
		case err == store.ErrNotFound:
			respondErr(w, errs.New(errs.CodeNotFound, "delivery not found"))
		default:
			if ee, ok := err.(*errs.Error); ok {
				respondErr(w, ee)
				return
			}
			respondErr(w, errs.New(errs.CodeInternal, "failed to retry delivery"))
		}
		return
	}
	respondJSON(w, http.StatusOK, dv)
}
