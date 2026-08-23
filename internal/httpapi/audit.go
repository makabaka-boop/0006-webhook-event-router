package httpapi

import (
	"net/http"
	"strconv"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
)

// handleListAudit 查询配置变更审计日志，支持按实体类型与实体 ID 过滤。
// GET /api/v1/audit?entity_type=&entity_id=&page=&limit=
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entityType := q.Get("entity_type")
	entityID, _ := strconv.ParseInt(q.Get("entity_id"), 10, 64)
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 50
	}
	logs, err := s.store.ListChangeLogs(r.Context(), entityType, entityID, page, limit)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to list audit logs"))
		return
	}
	if logs == nil {
		logs = []*domain.ChangeLog{}
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"audit_logs": logs,
		"page":       page,
		"limit":      limit,
	})
}
