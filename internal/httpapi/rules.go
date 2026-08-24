package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
)

type conditionRequest struct {
	Path     string             `json:"path"`
	Op       string             `json:"op"`
	Value    any                `json:"value"`
	Children []conditionRequest `json:"children"`
}

type ruleRequest struct {
	Name      string             `json:"name"`
	SourceID  *int64             `json:"source_id"`
	EventType string             `json:"event_type"`
	Condition []conditionRequest `json:"condition"`
	TargetID  int64              `json:"target_id"`
	TargetIDs []int64            `json:"target_ids"`
	Priority  int                `json:"priority"`
	Enabled   *bool              `json:"enabled"`
}

func toConditions(cs []conditionRequest) []domain.Condition {
	out := make([]domain.Condition, 0, len(cs))
	for _, c := range cs {
		node := domain.Condition{Path: c.Path, Op: c.Op, Value: c.Value}
		if len(c.Children) > 0 {
			node.Children = toConditions(c.Children)
		}
		out = append(out, node)
	}
	return out
}

// resolveRuleTargetIDs 归一化规则目标列表：优先 target_ids，兼容单目标 target_id。
func resolveRuleTargetIDs(targetID int64, targetIDs []int64) []int64 {
	if len(targetIDs) > 0 {
		return targetIDs
	}
	if targetID != 0 {
		return []int64{targetID}
	}
	return nil
}

// primaryTargetID 返回规则表 target_id 列所需的主目标值。
func primaryTargetID(targetID int64, targetIDs []int64) int64 {
	if targetID != 0 {
		return targetID
	}
	if len(targetIDs) > 0 {
		return targetIDs[0]
	}
	return 0
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req ruleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "invalid body"))
		return
	}
	if req.Name == "" || (req.TargetID == 0 && len(req.TargetIDs) == 0) {
		respondErr(w, errs.New(errs.CodeInvalidInput, "name and target_id/target_ids are required"))
		return
	}
	rule := &domain.Rule{
		Name:      req.Name,
		SourceID:  req.SourceID,
		EventType: req.EventType,
		Condition: toConditions(req.Condition),
		TargetID:  req.TargetID,
		Priority:  req.Priority,
		Enabled:   true,
	}
	rule.TargetIDs = resolveRuleTargetIDs(req.TargetID, req.TargetIDs)
	rule.TargetID = primaryTargetID(req.TargetID, req.TargetIDs)
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if err := s.store.CreateRule(r.Context(), rule); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to create rule"))
		return
	}
	respondJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRules(r.Context())
	if err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "internal error"))
		return
	}
	if list == nil {
		list = []*domain.Rule{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"rules": list})
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		s.respondNotFound(w, err, "rule")
		return
	}
	respondJSON(w, http.StatusOK, rule)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	existing, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		s.respondNotFound(w, err, "rule")
		return
	}
	var req ruleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "invalid body"))
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.SourceID != nil {
		existing.SourceID = req.SourceID
	}
	if req.EventType != "" {
		existing.EventType = req.EventType
	}
	if req.Condition != nil {
		existing.Condition = toConditions(req.Condition)
	}
	if req.TargetID != 0 {
		existing.TargetID = req.TargetID
	}
	if len(req.TargetIDs) > 0 {
		existing.TargetIDs = req.TargetIDs
	} else if req.TargetID != 0 {
		existing.TargetIDs = []int64{req.TargetID}
	}
	existing.Priority = req.Priority
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if err := s.store.UpdateRule(r.Context(), existing); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to update rule"))
		return
	}
	respondJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	if err := s.store.DeleteRule(r.Context(), id); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to delete rule"))
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
