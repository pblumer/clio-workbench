package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pblumer/clio-workbench/internal/model"
	"github.com/pblumer/clio-workbench/internal/scenario"
	"github.com/pblumer/clio-workbench/internal/store"
)

// handleImportDraft creates a draft in the local store from JSON supplied
// either inline (pasted into a textarea) or fetched from an arbitrary URL.
// Both paths exist so a Workbench reached only through a browser — e.g. a
// hosted/SaaS deployment with no shell or filesystem access — can still load
// example drafts without manual file copying.
func (s *Server) handleImportDraft(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	body, _, err := importBody(r.Context(), r)
	if err != nil {
		s.renderDraftsWithMessage(w, err.Error())
		return
	}

	var draft model.Draft
	if err := json.Unmarshal(body, &draft); err != nil {
		s.renderDraftsWithMessage(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if err := draft.Validate(); err != nil {
		s.renderDraftsWithMessage(w, fmt.Sprintf("validation failed: %v", err))
		return
	}

	if err := s.store.Create(&draft); err != nil {
		if errors.Is(err, store.ErrExists) {
			s.renderDraftsWithMessage(w, fmt.Sprintf("draft %q already exists", draft.ID))
			return
		}
		s.serverError(w, "import draft", err)
		return
	}

	s.log.Info("draft imported", "id", draft.ID)
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	s.render(w, "drafts.html", drafts)
}

// handleImportScenario saves a scenario suite into the local scenario store
// from JSON supplied either inline (pasted) or fetched from an arbitrary URL —
// the suite counterpart to handleImportDraft, so the Studio is fully usable
// from a browser-only (e.g. SaaS) Workbench.
func (s *Server) handleImportScenario(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	draftID := strings.TrimSpace(r.FormValue("draft"))

	body, _, err := importBody(r.Context(), r)
	if err != nil {
		s.renderScenarioImportError(w, err.Error())
		return
	}

	var suite scenario.Suite
	if err := json.Unmarshal(body, &suite); err != nil {
		s.renderScenarioImportError(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}
	if err := suite.Validate(); err != nil {
		s.renderScenarioImportError(w, fmt.Sprintf("validation failed: %v", err))
		return
	}

	// Snapshot the current draft revision if the suite has none and the draft exists.
	if draftID != "" && suite.DraftRev == "" {
		if d, err := s.store.Get(draftID); err == nil {
			suite.DraftRev = scenario.DraftRev(*d)
		}
	}

	if err := s.scenarios.Save(&suite); err != nil {
		s.renderScenarioImportError(w, fmt.Sprintf("save failed: %v", err))
		return
	}

	s.log.Info("scenario suite imported", "id", suite.ID)
	s.renderScenario(w, draftID, suite.ID)
}

// importBody resolves the JSON payload for an import request. A non-empty
// "json" form field (pasted content) wins; otherwise the "url" field is
// fetched. It returns the raw bytes and a human-readable source label, or a
// user-facing error if neither field is usable.
func importBody(ctx context.Context, r *http.Request) (body []byte, source string, err error) {
	if pasted := strings.TrimSpace(r.FormValue("json")); pasted != "" {
		return []byte(pasted), "pasted JSON", nil
	}
	urlStr := strings.TrimSpace(r.FormValue("url"))
	if urlStr == "" {
		return nil, "", errors.New("paste JSON or provide an import URL")
	}
	body, err = fetchURL(ctx, urlStr)
	if err != nil {
		return nil, "", fmt.Errorf("fetch failed: %v", err)
	}
	return body, urlStr, nil
}

// fetchURL performs a GET with a 10-second timeout. It accepts any 2xx status.
func fetchURL(ctx context.Context, urlStr string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// renderDraftsWithMessage renders the drafts list with an inline message.
func (s *Server) renderDraftsWithMessage(w http.ResponseWriter, msg string) {
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<p class="panel-hint bpmn-state bpmn-state-error">%s</p>`, msg)
	_ = s.tmpl.ExecuteTemplate(w, "drafts.html", drafts)
}

// renderScenarioImportError renders a small error fragment into the scenario panel.
func (s *Server) renderScenarioImportError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<p class="panel-hint bpmn-state bpmn-state-error">%s</p>`, msg)
}
