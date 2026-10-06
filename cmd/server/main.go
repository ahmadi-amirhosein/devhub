package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"devhub/internal/ai"
	"devhub/internal/auth"
	"devhub/internal/config"
	"devhub/internal/httpx"
	"devhub/internal/market"
	"devhub/internal/projects"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}

	var analyzer ai.Analyzer
	switch cfg.AIBackend {
	case "stub":
		analyzer = ai.Stub{}
	case "ollama":
		analyzer = ai.NewOllama(cfg.OllamaURL, cfg.OllamaModel)
	case "gemini":
		analyzer = ai.NewGemini(cfg.GeminiKey, cfg.GeminiModel)
	case "claude":
		analyzer = ai.NewClaude(cfg.AnthropicKey, cfg.AnthropicModel)
	default:
		return fmt.Errorf("unknown AI_BACKEND %q (use stub, ollama, gemini or claude)", cfg.AIBackend)
	}

	authH := &auth.Handler{DB: pool, Secret: cfg.JWTSecret}
	projH := projects.NewHandler(projects.NewRepo(pool), analyzer, 2)
	mktH := market.NewHandler(market.NewRepo(pool))
	protect := auth.Middleware(cfg.JWTSecret)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			httpx.Error(w, http.StatusServiceUnavailable, "db unavailable")
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST /api/auth/register", authH.Register)
	mux.HandleFunc("POST /api/auth/login", authH.Login)

	// public
	mux.HandleFunc("GET /api/projects", projH.List)
	mux.HandleFunc("GET /api/projects/{id}", projH.Get)

	// clients only
	mux.Handle("POST /api/projects", protect(auth.RequireRole("client", projH.Create)))
	mux.Handle("GET /api/me/projects", protect(auth.RequireRole("client", projH.Mine)))
	mux.Handle("POST /api/projects/{id}/analyze", protect(auth.RequireRole("client", projH.Analyze)))
	mux.Handle("POST /api/projects/{id}/publish", protect(auth.RequireRole("client", projH.Publish)))
	mux.Handle("GET /api/projects/{id}/proposals", protect(auth.RequireRole("client", mktH.ListProjectProposals)))
	mux.Handle("POST /api/proposals/{id}/accept", protect(auth.RequireRole("client", mktH.Accept)))

	// developers only
	mux.Handle("GET /api/me/profile", protect(auth.RequireRole("developer", mktH.GetProfile)))
	mux.Handle("PUT /api/me/profile", protect(auth.RequireRole("developer", mktH.PutProfile)))
	mux.Handle("POST /api/projects/{id}/proposals", protect(auth.RequireRole("developer", mktH.CreateProposal)))
	mux.Handle("GET /api/me/proposals", protect(auth.RequireRole("developer", mktH.MyProposals)))
	mux.Handle("POST /api/proposals/{id}/withdraw", protect(auth.RequireRole("developer", mktH.Withdraw)))

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           logRequests(cors(cfg.CORSOrigin, mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      3 * time.Minute, // analyze can take a while on CPU
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr, "ai", cfg.AIBackend)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

func cors(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds())
	})
}
