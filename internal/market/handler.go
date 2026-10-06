package market

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"devhub/internal/auth"
	"devhub/internal/httpx"
)

type Handler struct{ repo *Repo }

func NewHandler(repo *Repo) *Handler { return &Handler{repo: repo} }

func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
	case errors.Is(err, ErrDuplicate):
		httpx.Error(w, http.StatusConflict, "you already sent a proposal for this project")
	case errors.Is(err, ErrConflict):
		httpx.Error(w, http.StatusConflict, "this proposal or project is no longer open")
	default:
		slog.Error("market", "err", err)
		httpx.Error(w, http.StatusInternalServerError, "internal error")
	}
}

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	c, _ := auth.FromContext(r.Context())
	p, err := h.repo.GetProfile(r.Context(), c.UserID())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) PutProfile(w http.ResponseWriter, r *http.Request) {
	c, _ := auth.FromContext(r.Context())
	var in Profile
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.UserID = c.UserID()
	in.Headline = strings.TrimSpace(in.Headline)
	in.Bio = strings.TrimSpace(in.Bio)
	in.Skills = cleanSkills(in.Skills)
	if n := utf8.RuneCountInString(in.Headline); n < 3 || n > 100 {
		httpx.Error(w, http.StatusBadRequest, "headline must be 3-100 characters")
		return
	}
	if utf8.RuneCountInString(in.Bio) > 2000 {
		httpx.Error(w, http.StatusBadRequest, "bio must be at most 2000 characters")
		return
	}
	if len(in.Skills) == 0 {
		httpx.Error(w, http.StatusBadRequest, "add at least one skill")
		return
	}
	if in.HourlyRate < 0 || in.HourlyRate > 10000 {
		httpx.Error(w, http.StatusBadRequest, "invalid hourly rate")
		return
	}
	p, err := h.repo.UpsertProfile(r.Context(), in)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

type proposalInput struct {
	Price   int    `json:"price"`
	Message string `json:"message"`
}

func (h *Handler) CreateProposal(w http.ResponseWriter, r *http.Request) {
	c, _ := auth.FromContext(r.Context())
	var in proposalInput
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Message = strings.TrimSpace(in.Message)
	if in.Price <= 0 || in.Price > 10_000_000 {
		httpx.Error(w, http.StatusBadRequest, "invalid price")
		return
	}
	if n := utf8.RuneCountInString(in.Message); n < 20 || n > 2000 {
		httpx.Error(w, http.StatusBadRequest, "message must be 20-2000 characters")
		return
	}
	if _, err := h.repo.GetProfile(r.Context(), c.UserID()); errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusBadRequest, "create your profile before sending proposals")
		return
	} else if err != nil {
		h.fail(w, err)
		return
	}
	p, err := h.repo.CreateProposal(r.Context(), r.PathValue("id"), c.UserID(), in.Price, in.Message)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, p)
}

// ListProjectProposals: only the client who owns the project may see them.
func (h *Handler) ListProjectProposals(w http.ResponseWriter, r *http.Request) {
	c, _ := auth.FromContext(r.Context())
	id := r.PathValue("id")
	ok, err := h.repo.IsProjectOwner(r.Context(), id, c.UserID())
	if err != nil {
		h.fail(w, err)
		return
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	items, err := h.repo.ListForProject(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) MyProposals(w http.ResponseWriter, r *http.Request) {
	c, _ := auth.FromContext(r.Context())
	items, err := h.repo.ListByDeveloper(r.Context(), c.UserID())
	if err != nil {
		h.fail(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) Accept(w http.ResponseWriter, r *http.Request) {
	c, _ := auth.FromContext(r.Context())
	if err := h.repo.Accept(r.Context(), r.PathValue("id"), c.UserID()); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Withdraw(w http.ResponseWriter, r *http.Request) {
	c, _ := auth.FromContext(r.Context())
	if err := h.repo.Withdraw(r.Context(), r.PathValue("id"), c.UserID()); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func cleanSkills(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || utf8.RuneCountInString(s) > 30 || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) == 15 {
			break
		}
	}
	return out
}
