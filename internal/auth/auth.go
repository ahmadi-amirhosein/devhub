package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"devhub/internal/httpx"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type ctxKey struct{}

type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func (c *Claims) UserID() string { return c.Subject }

func Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func Check(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func NewToken(secret, userID, role string, ttl time.Duration) (string, error) {
	now := time.Now()
	c := Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
}

// Middleware validates the Bearer token and stores the claims in the request context.
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims := &Claims{}
			tok, err := jwt.ParseWithClaims(raw, claims,
				func(*jwt.Token) (any, error) { return []byte(secret), nil },
				jwt.WithValidMethods([]string{"HS256"}),
			)
			if err != nil || !tok.Valid {
				httpx.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, claims)))
		})
	}
}

func FromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(*Claims)
	return c, ok
}

// RequireRole must be used behind Middleware.
func RequireRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := FromContext(r.Context())
		if !ok || c.Role != role {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	}
}
