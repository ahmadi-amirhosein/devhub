package projects

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("project not found")

type Project struct {
	ID          string          `json:"id"`
	ClientID    string          `json:"client_id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Category    string          `json:"category"`
	Status      string          `json:"status"`
	BudgetMin   int             `json:"budget_min"`
	BudgetMax   int             `json:"budget_max"`
	Spec        json.RawMessage `json:"spec,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

const cols = `id::text, client_id::text, title, description, category, status, budget_min, budget_max, spec, created_at`

type Repo struct{ db *pgxpool.Pool }

func NewRepo(db *pgxpool.Pool) *Repo { return &Repo{db: db} }

func scan(row pgx.Row) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.ClientID, &p.Title, &p.Description, &p.Category,
		&p.Status, &p.BudgetMin, &p.BudgetMax, &p.Spec, &p.CreatedAt)
	return p, err
}

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "22P02" { // malformed uuid
		return ErrNotFound
	}
	return err
}

func (r *Repo) Create(ctx context.Context, p Project) (Project, error) {
	row := r.db.QueryRow(ctx,
		`INSERT INTO projects (client_id, title, description, category, budget_min, budget_max)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+cols,
		p.ClientID, p.Title, p.Description, p.Category, p.BudgetMin, p.BudgetMax)
	return scan(row)
}

func (r *Repo) Get(ctx context.Context, id string) (Project, error) {
	p, err := scan(r.db.QueryRow(ctx, `SELECT `+cols+` FROM projects WHERE id = $1`, id))
	return p, mapErr(err)
}

func (r *Repo) collect(ctx context.Context, q string, args ...any) ([]Project, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Project, error) { return scan(row) })
}

func (r *Repo) ListOpen(ctx context.Context, category string, limit, offset int) ([]Project, error) {
	return r.collect(ctx,
		`SELECT `+cols+` FROM projects
		 WHERE status = 'open' AND ($1 = '' OR category = $1)
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, category, limit, offset)
}

func (r *Repo) ListByClient(ctx context.Context, clientID string) ([]Project, error) {
	return r.collect(ctx,
		`SELECT `+cols+` FROM projects WHERE client_id = $1 ORDER BY created_at DESC`, clientID)
}

func (r *Repo) SaveSpec(ctx context.Context, id, category string, spec []byte) error {
	_, err := r.db.Exec(ctx,
		`UPDATE projects SET spec = $2, category = CASE WHEN $3 <> '' THEN $3 ELSE category END WHERE id = $1`,
		id, spec, category)
	return err
}

func (r *Repo) Publish(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `UPDATE projects SET status = 'open' WHERE id = $1 AND status = 'draft'`, id)
	return err
}
