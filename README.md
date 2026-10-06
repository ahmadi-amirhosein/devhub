# DevHub backend (Go)

Core API for the specialized-programming-work platform (evolution of ScopeSense).

## Run

```bash
docker compose up -d      # Postgres 16 + pgvector; applies migrations/ on first start
cp .env.example .env      # then export the variables in your shell
go mod tidy               # resolves dependency versions and writes go.sum
go vet ./...
go run ./cmd/server
```

## Try it

```bash
curl -s localhost:8080/api/auth/register -d '{"email":"a@b.com","password":"password123","role":"client"}'
# copy the token into $TOKEN, then:
curl -s localhost:8080/api/projects -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"Android app for a clinic","description":"We need a booking app for our clinic with push reminders and a simple admin panel.","budget_min":500,"budget_max":1500}'
curl -s -X POST localhost:8080/api/projects/$ID/analyze -H "Authorization: Bearer $TOKEN"
curl -s -X POST localhost:8080/api/projects/$ID/publish -H "Authorization: Bearer $TOKEN"
curl -s localhost:8080/api/projects
```

`AI_BACKEND` picks the analyzer: `stub` (default, no model), `ollama` (local), `gemini` (needs `GEMINI_API_KEY`) or `claude` (needs `ANTHROPIC_API_KEY`). Models are set with `GEMINI_MODEL` / `ANTHROPIC_MODEL`. Cloud backends send the project description to the provider; keys are read from the environment only.

## Layout

- `cmd/server`: wiring, routes, CORS, request log, graceful shutdown
- `internal/auth`: bcrypt, JWT, role middleware, register/login
- `internal/projects`: repository (SQL) and HTTP handlers
- `internal/ai`: `Analyzer` interface with `Stub` and `Ollama` implementations
- `internal/market`: developer profiles and proposals (send, list, accept, withdraw)
- `migrations`: SQL schema (001 users/projects/pgvector, 002 profiles/proposals). On an existing database apply new files by hand: `Get-Content migrations\002_market.sql | docker compose exec -T db psql -U devhub -d devhub`
- `scripts/test-market.ps1`: end-to-end check of the proposal flow and access rules

## Notes

- Written without a Go toolchain available, so it has not been compiled: run `go mod tidy && go vet ./...` first and fix anything it reports.
- Next: proposals, developer profiles, embeddings table for matching, rate limiting on auth, refresh tokens.
