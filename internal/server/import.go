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

// handleImportDraftFromURL fetches a JSON draft from an arbitrary URL and
// creates it in the local store. The primary use-case is pulling example
// drafts (e.g. from GitHub raw) into the Workbench without manual file
// copying.
func (s *Server) handleImportDraftFromURL(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	urlStr := strings.TrimSpace(r.FormValue("url"))
	if urlStr == "" {
		s.renderDraftsWithMessage(w, "Import URL required")
		return
	}

	body, err := fetchURL(r.Context(), urlStr)
	if err != nil {
		s.renderDraftsWithMessage(w, fmt.Sprintf("fetch failed: %v", err))
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

	s.log.Info("draft imported from URL", "id", draft.ID, "url", urlStr)
	drafts, err := s.store.List()
	if err != nil {
		s.serverError(w, "list drafts", err)
		return
	}
	s.render(w, "drafts.html", drafts)
}

// handleImportScenarioFromURL fetches a JSON scenario suite from an arbitrary
// URL and saves it into the local scenario store.
func (s *Server) handleImportScenarioFromURL(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	urlStr := strings.TrimSpace(r.FormValue("url"))
	draftID := strings.TrimSpace(r.FormValue("draft"))
	if urlStr == "" {
		s.renderScenarioImportError(w, "Import URL required")
		return
	}

	body, err := fetchURL(r.Context(), urlStr)
	if err != nil {
		s.renderScenarioImportError(w, fmt.Sprintf("fetch failed: %v", err))
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

	s.log.Info("scenario suite imported from URL", "id", suite.ID, "url", urlStr)
	s.renderScenario(w, draftID, suite.ID)
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
