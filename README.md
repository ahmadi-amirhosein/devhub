# DevHub

A marketplace backend for specialized programming work. Clients describe a project in plain language, an LLM turns it into a structured spec, and developers send proposals that the client can accept.

DevHub grew out of [ScopeSense](https://github.com/ahmadi-amirhosein), an agentic RAG pipeline that turns a vague client description into a spec and a data-backed price verdict. This repository is the platform around that idea: accounts, projects, proposals, and a pluggable AI layer.

> **Status:** working prototype and portfolio project. The core flow works end to end. Hardening and AI matching are not done yet, see [Roadmap](#roadmap).

Frontend: [devhub-web](https://github.com/ahmadi-amirhosein/devhub-web) (Next.js).

## What works today

- Registration and login with two roles, **client** and **developer** (bcrypt + JWT).
- Clients create projects, run an **AI analysis** that returns a structured spec (summary, category, features, risks), and publish them.
- Public, paginated listing of open projects with a category filter.
- Developers keep a profile (headline, skills, hourly rate) and send **one proposal per project**.
- Clients see the proposals on their own projects and **accept** one. The other pending proposals are rejected and the project moves to `in_progress`, atomically.
- Developers can withdraw a pending proposal and see the status of all their proposals.

## Architecture

```mermaid
flowchart LR
  Web["Next.js web app"] -->|"JSON over HTTP, JWT"| API["Go API"]
  API --> DB[("PostgreSQL 16 + pgvector")]
  API --> AI{{"Analyzer interface"}}
  AI --> Stub["Stub (no model)"]
  AI --> Ollama["Ollama (local)"]
  AI --> Gemini["Google Gemini"]
  AI --> Claude["Anthropic Claude"]
```

```
cmd/server          wiring, routes, CORS, request log, graceful shutdown
internal/auth       password hashing, JWT, role middleware, register/login
internal/projects   project repository (SQL) and HTTP handlers, AI analysis endpoint
internal/market     developer profiles and proposals
internal/ai         Analyzer interface and its implementations
internal/config     environment-based configuration
internal/httpx      small JSON helpers
migrations          SQL schema, applied in order
scripts             end-to-end test script (PowerShell)
```

## Engineering decisions

- **The AI layer is an interface, not a dependency.** `ai.Analyzer` has one method. Switching between a local Ollama model, Gemini, Claude, or a no-model stub is one environment variable, and a future Python service can be added as another implementation without touching the rest of the code.
- **Untrusted text is treated as data.** Client descriptions are wrapped in delimiters and the system prompt says to ignore instructions inside them. Model output is parsed defensively (JSON extracted from the reply) and every field is length-bounded before it reaches the database.
- **Accepting a proposal is one transaction.** The project row is locked with `SELECT ... FOR UPDATE`, the accept is a guarded `UPDATE ... WHERE status = 'pending'`, and the remaining pending proposals are rejected in the same transaction. Two simultaneous accepts cannot both win.
- **Bounded AI concurrency.** At most two analyses run at once, each with a two-minute timeout. When the server is busy it answers `429` with `Retry-After` instead of piling up requests.
- **No resource enumeration.** Foreign or missing projects both return `404`, and login returns the same error for an unknown email and a wrong password.
- **Defensive HTTP defaults.** 1 MB body limit, unknown JSON fields rejected, server read/write/idle timeouts, graceful shutdown on SIGINT/SIGTERM, pagination limits clamped.
- **Unicode-aware validation.** Length checks count characters, not bytes, so Persian and other non-Latin text is validated correctly.
- **Plain standard library routing.** Go 1.22 `net/http` pattern routing (`"POST /api/projects/{id}/analyze"`), `pgx` for Postgres, `log/slog` for structured logs. No web framework.

## Run it

Requirements: Go 1.22+, Docker.

```powershell
# 1. database (PostgreSQL 16 with pgvector on localhost:5433, schema applied on first start)
docker compose up -d

# 2. configuration: the server reads environment variables only (.env.example lists them)
$env:DATABASE_URL = "postgres://devhub:devhub@localhost:5433/devhub?sslmode=disable"
$env:JWT_SECRET   = "a-long-random-string-of-at-least-32-characters"
$env:AI_BACKEND   = "stub"        # stub | ollama | gemini | claude

# 3. run
go mod tidy
go run ./cmd/server
```

Check `http://localhost:8080/healthz`. On macOS/Linux use `export NAME=value` instead of `$env:NAME = "value"`.

If the database was created before a migration was added, apply the new file by hand:

```powershell
Get-Content migrations\002_market.sql | docker compose exec -T db psql -U devhub -d devhub
```

### AI backends

| `AI_BACKEND` | Needs | Notes |
|---|---|---|
| `stub` | nothing | default, returns a trivial spec, good for development |
| `ollama` | a running Ollama, `OLLAMA_MODEL` | local and free, default model `qwen2.5:7b-instruct` |
| `gemini` | `GEMINI_API_KEY` | model set with `GEMINI_MODEL` |
| `claude` | `ANTHROPIC_API_KEY` | model set with `ANTHROPIC_MODEL` |

The cloud backends send the project description to the provider. API keys are read from the environment only and never logged.

## API

Authenticated routes expect `Authorization: Bearer <token>`.

| Method | Path | Who | Purpose |
|---|---|---|---|
| POST | `/api/auth/register` | public | create an account (`client` or `developer`) |
| POST | `/api/auth/login` | public | get a token |
| GET | `/api/projects` | public | open projects, `?category=&limit=&offset=` |
| GET | `/api/projects/{id}` | public | one non-draft project |
| POST | `/api/projects` | client | create a draft |
| GET | `/api/me/projects` | client | my projects |
| POST | `/api/projects/{id}/analyze` | client (owner) | AI-structured spec |
| POST | `/api/projects/{id}/publish` | client (owner) | draft to open |
| GET | `/api/projects/{id}/proposals` | client (owner) | proposals with sender profile |
| POST | `/api/proposals/{id}/accept` | client (owner) | accept one, reject the rest |
| GET / PUT | `/api/me/profile` | developer | read or save my profile |
| POST | `/api/projects/{id}/proposals` | developer | send a proposal |
| GET | `/api/me/proposals` | developer | my proposals |
| POST | `/api/proposals/{id}/withdraw` | developer | withdraw a pending proposal |

## Testing

`scripts/test-market.ps1` runs the whole proposal flow against a running server and checks the access rules: a developer without a profile cannot propose, duplicate proposals are refused, another client cannot see or accept proposals, accepting twice is refused, and the final statuses are correct.

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\test-market.ps1
```

There are no Go unit tests yet.

## Roadmap

Not done yet, in rough priority order:

- Token in an httpOnly cookie instead of browser storage, refresh tokens, and rate limiting on login.
- Go unit and integration tests, and a CI workflow.
- **AI matching:** embeddings for projects and developer profiles stored in pgvector, with ranked suggestions for both sides. The `vector` extension is already enabled in the schema but not used yet.
- Dockerfile for the API and a full compose setup.
- Messaging, milestones and payments.

## License

MIT
