package projects

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"devhub/internal/ai"
	"devhub/internal/auth"
	"devhub/internal/httpx"
)

type Handler struct {
	repo     *Repo
	analyzer ai.Analyzer
	aiSlots  chan struct{} // bounds how many AI analyses run at once
}

func NewHandler(repo *Repo, analyzer ai.Analyzer, maxConcurrentAI int) *Handler {
	return &Handler{repo: repo, analyzer: analyzer, aiSlots: make(chan struct{}, maxConcurrentAI)}
}

type createInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	BudgetMin   int    `json:"budget_min"`
	BudgetMax   int    `json:"budget_max"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	var in createInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if n := utf8.RuneCountInString(in.Title); n < 5 || n > 120 {
		httpx.Error(w, http.StatusBadRequest, "title must be 5-120 characters")
		return
	}
	if n := utf8.RuneCountInString(in.Description); n < 30 || n > 5000 {
		httpx.Error(w, http.StatusBadRequest, "description must be 30-5000 characters")
		return
	}
	if in.BudgetMin < 0 || in.BudgetMax < in.BudgetMin {
		httpx.Error(w, http.StatusBadRequest, "invalid budget range")
		return
	}
	p, err := h.repo.Create(r.Context(), Project{
		ClientID: claims.UserID(), Title: in.Title, Description: in.Description,
		BudgetMin: in.BudgetMin, BudgetMax: in.BudgetMax,
	})
	if err != nil {
		slog.Error("create project", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusCreated, p)
}

// List is public: only open projects, optional ?category=, paginated.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	limit := clamp(atoi(r.URL.Query().Get("limit"), 20), 1, 50)
	offset := max(atoi(r.URL.Query().Get("offset"), 0), 0)
	items, err := h.repo.ListOpen(r.Context(), r.URL.Query().Get("category"), limit, offset)
	if err != nil {
		slog.Error("list projects", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (h *Handler) Mine(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.FromContext(r.Context())
	items, err := h.repo.ListByClient(r.Context(), claims.UserID())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// Get is public but only exposes non-draft projects.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	p, err := h.repo.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) || (err == nil && p.Status == "draft") {
		httpx.Error(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	p, ok := h.owned(w, r)
	if !ok {
		return
	}
	if err := h.repo.Publish(r.Context(), p.ID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Analyze asks the AI layer for a structured spec and stores it on the project.
func (h *Handler) Analyze(w http.ResponseWriter, r *http.Request) {
	p, ok := h.owned(w, r)
	if !ok {
		return
	}
	select {
	case h.aiSlots <- struct{}{}:
		defer func() { <-h.aiSlots }()
	default:
		w.Header().Set("Retry-After", "10")
		httpx.Error(w, http.StatusTooManyRequests, "AI is busy, try again shortly")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	spec, err := h.analyzer.Analyze(ctx, p.Description)
	if err != nil {
		slog.Error("analyze", "project", p.ID, "err", err)
		httpx.Error(w, http.StatusBadGateway, "analysis failed")
		return
	}
	raw, _ := json.Marshal(spec)
	if err := h.repo.SaveSpec(r.Context(), p.ID, spec.Category, raw); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, spec)
}

// owned loads the project and checks it belongs to the caller.
// Missing and foreign projects both return 404 so ids can't be probed.
func (h *Handler) owned(w http.ResponseWriter, r *http.Request) (Project, bool) {
	claims, _ := auth.FromContext(r.Context())
	p, err := h.repo.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, ErrNotFound) || (err == nil && p.ClientID != claims.UserID()) {
		httpx.Error(w, http.StatusNotFound, "project not found")
		return Project{}, false
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return Project{}, false
	}
	return p, true
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }
