package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
)

type targetRequest struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled *bool  `json:"enabled"`
}

func (s *Server) handleCreateTarget(w http.ResponseWriter, r *http.Request) {
	var req targetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "invalid body"))
		return
	}
	if req.Name == "" || req.URL == "" {
		respondErr(w, errs.New(errs.CodeInvalidInput, "name and url are required"))
		return
	}
	t := &domain.Target{Name: req.Name, URL: req.URL, Enabled: true}
	if req.Enabled != nil {
		t.Enabled = *req.Enabled
	}
	if err := s.store.CreateTarget(r.Context(), t); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to create target"))
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

func (s *Server) handleListTargets(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTargets(r.Context())
	if err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "internal error"))
		return
	}
	if list == nil {
		list = []*domain.Target{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"targets": list})
}

func (s *Server) handleGetTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	t, err := s.store.GetTarget(r.Context(), id)
	if err != nil {
		s.respondNotFound(w, err, "target")
		return
	}
	respondJSON(w, http.StatusOK, t)
}

func (s *Server) handleUpdateTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	existing, err := s.store.GetTarget(r.Context(), id)
	if err != nil {
		s.respondNotFound(w, err, "target")
		return
	}
	var req targetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "invalid body"))
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.URL != "" {
		existing.URL = req.URL
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if err := s.store.UpdateTarget(r.Context(), existing); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to update target"))
		return
	}
	respondJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteTarget(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	if err := s.store.DeleteTarget(r.Context(), id); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to delete target"))
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
