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

// deliveryContextKey 是请求上下文中挂载出站投递上下文的键。
type deliveryContextKey struct{}

// withDeliveryContext 在父上下文 parent 中挂载独立的出站投递上下文 dctx。
func withDeliveryContext(parent context.Context, dctx context.Context) context.Context {
	return context.WithValue(parent, deliveryContextKey{}, dctx)
}

// deliveryContextOf 从请求上下文中取出出站投递上下文。
//
// 若调用方未通过 withDeliveryContext 显式挂载出站上下文，或挂载的值为空，
// 则回退到请求上下文本身，保证下游 dispatcher 始终能拿到一个可用的 context。
func deliveryContextOf(ctx context.Context) context.Context {
	if dctx, ok := ctx.Value(deliveryContextKey{}).(context.Context); ok && dctx != nil {
		return dctx
	}
	return ctx
}

// newDeliveryContext 为一次请求的出站转发建立独立的生命周期。
//
// 出站转发与请求处理链共享同一个父上下文，但拥有自己的取消信号，供
// dispatcher 在构造出站 HTTP 请求时通过 http.NewRequestWithContext 使用。
func newDeliveryContext(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	return ctx
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			w.Header().Set("Content-Type", "application/json")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		// 将出站投递上下文挂载到请求上下文，供处理链按需取用。
		ctx = withDeliveryContext(ctx, newDeliveryContext(r.Context()))

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
