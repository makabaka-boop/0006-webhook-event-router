package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"webhook-event-router/internal/config"
	"webhook-event-router/internal/dispatcher"
	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
	"webhook-event-router/internal/metrics"
	"webhook-event-router/internal/store"
)

// AttemptStore 供查询投递尝试明细的最小依赖。
type AttemptStore interface {
	ListByDelivery(ctx context.Context, deliveryID int64) ([]*domain.DeliveryAttempt, error)
}

// Server 聚合 handler 依赖。
type Server struct {
	store      store.Store
	cfg        *config.Config
	dispatcher *dispatcher.Dispatcher
	metrics    *metrics.Registry
	attempts   AttemptStore
}

// NewServer 构造 Server 并注册路由。
func NewServer(st store.Store, cfg *config.Config, d *dispatcher.Dispatcher, reg *metrics.Registry, att AttemptStore) http.Handler {
	s := &Server{store: st, cfg: cfg, dispatcher: d, metrics: reg, attempts: att}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/events", s.handleCreateEvent)
	mux.HandleFunc("GET /api/v1/events/{id}", s.handleGetEvent)
	mux.HandleFunc("GET /api/v1/deliveries", s.handleListDeliveries)

	mux.HandleFunc("POST /api/v1/sources", s.handleCreateSource)
	mux.HandleFunc("GET /api/v1/sources", s.handleListSources)
	mux.HandleFunc("GET /api/v1/sources/{id}", s.handleGetSource)
	mux.HandleFunc("PUT /api/v1/sources/{id}", s.handleUpdateSource)
	mux.HandleFunc("DELETE /api/v1/sources/{id}", s.handleDeleteSource)

	mux.HandleFunc("POST /api/v1/rules", s.handleCreateRule)
	mux.HandleFunc("GET /api/v1/rules", s.handleListRules)
	mux.HandleFunc("GET /api/v1/rules/{id}", s.handleGetRule)
	mux.HandleFunc("PUT /api/v1/rules/{id}", s.handleUpdateRule)
	mux.HandleFunc("DELETE /api/v1/rules/{id}", s.handleDeleteRule)

	mux.HandleFunc("POST /api/v1/targets", s.handleCreateTarget)
	mux.HandleFunc("GET /api/v1/targets", s.handleListTargets)
	mux.HandleFunc("GET /api/v1/targets/{id}", s.handleGetTarget)
	mux.HandleFunc("PUT /api/v1/targets/{id}", s.handleUpdateTarget)
	mux.HandleFunc("DELETE /api/v1/targets/{id}", s.handleDeleteTarget)

	mux.HandleFunc("POST /api/v1/deliveries/{id}/retry", s.handleRetryDelivery)
	mux.HandleFunc("GET /api/v1/audit", s.handleListAudit)
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			w.Header().Set("Content-Type", "application/json")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// respondJSON 写成功响应。
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// respondErr 写统一错误响应。
func respondErr(w http.ResponseWriter, e *errs.Error) {
	status := e.Status
	if status == 0 {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"code": string(e.Code), "message": e.Message})
}
