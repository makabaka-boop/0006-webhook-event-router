package httpapi

import "net/http"

// handleMetrics 暴露 Prometheus 文本格式指标。
// GET /metrics
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		respondJSON(w, http.StatusOK, map[string]string{"status": "metrics disabled"})
		return
	}
	s.metrics.Handler().ServeHTTP(w, r)
}
