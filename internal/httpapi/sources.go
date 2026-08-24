package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"webhook-event-router/internal/domain"
	"webhook-event-router/internal/errs"
	"webhook-event-router/internal/store"
)

type sourceRequest struct {
	Name         string   `json:"name"`
	Secret       string   `json:"secret"`
	Enabled      *bool    `json:"enabled"`
	AllowedTypes []string `json:"allowed_event_types"`
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "invalid body"))
		return
	}
	if req.Name == "" || req.Secret == "" {
		respondErr(w, errs.New(errs.CodeInvalidInput, "name and secret are required"))
		return
	}
	src := &domain.Source{
		Name:         req.Name,
		Secret:       req.Secret,
		Enabled:      true,
		AllowedTypes: req.AllowedTypes,
	}
	if req.Enabled != nil {
		src.Enabled = *req.Enabled
	}
	if err := s.store.CreateSource(r.Context(), src); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to create source"))
		return
	}
	respondJSON(w, http.StatusCreated, src)
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSources(r.Context())
	if err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "internal error"))
		return
	}
	if list == nil {
		list = []*domain.Source{}
	}
	respondJSON(w, http.StatusOK, map[string]any{"sources": list})
}

func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	src, err := s.store.GetSource(r.Context(), id)
	if err != nil {
		s.respondNotFound(w, err, "source")
		return
	}
	respondJSON(w, http.StatusOK, src)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	existing, err := s.store.GetSource(r.Context(), id)
	if err != nil {
		s.respondNotFound(w, err, "source")
		return
	}
	var req sourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, errs.New(errs.CodePayloadInvalid, "invalid body"))
		return
	}
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Secret != "" {
		existing.Secret = req.Secret
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.AllowedTypes != nil {
		existing.AllowedTypes = req.AllowedTypes
	}
	if err := s.store.UpdateSource(r.Context(), existing); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to update source"))
		return
	}
	respondJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		respondErr(w, errs.New(errs.CodeInvalidInput, "invalid id"))
		return
	}
	if err := s.store.DeleteSource(r.Context(), id); err != nil {
		respondErr(w, errs.New(errs.CodeInternal, "failed to delete source"))
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) respondNotFound(w http.ResponseWriter, err error, entity string) {
	if err == store.ErrNotFound {
		respondErr(w, errs.New(errs.CodeNotFound, entity+" not found"))
		return
	}
	respondErr(w, errs.New(errs.CodeInternal, "internal error"))
}
