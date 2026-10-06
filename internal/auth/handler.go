package auth

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"devhub/internal/httpx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	DB     *pgxpool.Pool
	Secret string
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

const tokenTTL = 24 * time.Hour

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if _, err := mail.ParseAddress(in.Email); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid email")
		return
	}
	// bcrypt only uses the first 72 bytes, so longer passwords are rejected.
	if len(in.Password) < 8 || len(in.Password) > 72 {
		httpx.Error(w, http.StatusBadRequest, "password must be 8-72 bytes")
		return
	}
	if in.Role != "client" && in.Role != "developer" {
		httpx.Error(w, http.StatusBadRequest, "role must be client or developer")
		return
	}
	hash, err := Hash(in.Password)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	var id string
	err = h.DB.QueryRow(r.Context(),
		`INSERT INTO users (email, password_hash, role) VALUES ($1, $2, $3) RETURNING id::text`,
		in.Email, hash, in.Role).Scan(&id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		httpx.Error(w, http.StatusConflict, "email already registered")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.respondWithToken(w, http.StatusCreated, id, in.Role)
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !httpx.Decode(w, r, &in) {
		return
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	var id, hash, role string
	err := h.DB.QueryRow(r.Context(),
		`SELECT id::text, password_hash, role FROM users WHERE email = $1`, in.Email).
		Scan(&id, &hash, &role)
	// Same message for "no such user" and "wrong password" so emails can't be probed.
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !Check(hash, in.Password)) {
		httpx.Error(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	h.respondWithToken(w, http.StatusOK, id, role)
}

func (h *Handler) respondWithToken(w http.ResponseWriter, status int, id, role string) {
	tok, err := NewToken(h.Secret, id, role, tokenTTL)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, status, map[string]string{"token": tok, "role": role})
}
