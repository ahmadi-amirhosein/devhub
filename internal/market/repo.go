package market

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate")
	ErrConflict  = errors.New("conflict")
)

type Profile struct {
	UserID     string   `json:"user_id"`
	Headline   string   `json:"headline"`
	Bio        string   `json:"bio"`
	Skills     []string `json:"skills"`
	HourlyRate int      `json:"hourly_rate"`
}

type Proposal struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	DeveloperID string    `json:"developer_id"`
	Price       int       `json:"price"`
	Message     string    `json:"message"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProposalWithDev is what a client sees: the proposal plus who sent it.
type ProposalWithDev struct {
	Proposal
	Headline   string   `json:"developer_headline"`
	Skills     []string `json:"developer_skills"`
	HourlyRate int      `json:"developer_hourly_rate"`
}

// ProposalWithProject is what a developer sees in their own list.
type ProposalWithProject struct {
	Proposal
	ProjectTitle string `json:"project_title"`
}

type Repo struct{ db *pgxpool.Pool }

func NewRepo(db *pgxpool.Pool) *Repo { return &Repo{db: db} }

func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "22P02": // malformed uuid
			return ErrNotFound
		case "23505": // unique violation
			return ErrDuplicate
		}
	}
	return err
}

const propCols = `pr.id::text, pr.project_id::text, pr.developer_id::text, pr.price, pr.message, pr.status, pr.created_at`

func (r *Repo) UpsertProfile(ctx context.Context, p Profile) (Profile, error) {
	err := r.db.QueryRow(ctx,
		`INSERT INTO developer_profiles (user_id, headline, bio, skills, hourly_rate)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id) DO UPDATE SET headline = EXCLUDED.headline, bio = EXCLUDED.bio,
		   skills = EXCLUDED.skills, hourly_rate = EXCLUDED.hourly_rate, updated_at = now()
		 RETURNING user_id::text, headline, bio, skills, hourly_rate`,
		p.UserID, p.Headline, p.Bio, p.Skills, p.HourlyRate).
		Scan(&p.UserID, &p.Headline, &p.Bio, &p.Skills, &p.HourlyRate)
	return p, mapErr(err)
}

func (r *Repo) GetProfile(ctx context.Context, userID string) (Profile, error) {
	var p Profile
	err := r.db.QueryRow(ctx,
		`SELECT user_id::text, headline, bio, skills, hourly_rate FROM developer_profiles WHERE user_id = $1`, userID).
		Scan(&p.UserID, &p.Headline, &p.Bio, &p.Skills, &p.HourlyRate)
	return p, mapErr(err)
}

func (r *Repo) IsProjectOwner(ctx context.Context, projectID, clientID string) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1 AND client_id = $2)`, projectID, clientID).Scan(&ok)
	if errors.Is(mapErr(err), ErrNotFound) {
		return false, nil
	}
	return ok, err
}

// CreateProposal only succeeds while the project is open (otherwise ErrNotFound).
func (r *Repo) CreateProposal(ctx context.Context, projectID, devID string, price int, message string) (Proposal, error) {
	var p Proposal
	err := r.db.QueryRow(ctx,
		`INSERT INTO proposals (project_id, developer_id, price, message)
		 SELECT id, $2::uuid, $3::int, $4::text FROM projects WHERE id = $1 AND status = 'open'
		 RETURNING id::text, project_id::text, developer_id::text, price, message, status, created_at`,
		projectID, devID, price, message).
		Scan(&p.ID, &p.ProjectID, &p.DeveloperID, &p.Price, &p.Message, &p.Status, &p.CreatedAt)
	return p, mapErr(err)
}

func (r *Repo) ListForProject(ctx context.Context, projectID string) ([]ProposalWithDev, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+propCols+`, dp.headline, dp.skills, dp.hourly_rate
		 FROM proposals pr JOIN developer_profiles dp ON dp.user_id = pr.developer_id
		 WHERE pr.project_id = $1 ORDER BY pr.created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalWithDev, error) {
		var x ProposalWithDev
		err := row.Scan(&x.ID, &x.ProjectID, &x.DeveloperID, &x.Price, &x.Message, &x.Status, &x.CreatedAt,
			&x.Headline, &x.Skills, &x.HourlyRate)
		return x, err
	})
}

func (r *Repo) ListByDeveloper(ctx context.Context, devID string) ([]ProposalWithProject, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+propCols+`, p.title
		 FROM proposals pr JOIN projects p ON p.id = pr.project_id
		 WHERE pr.developer_id = $1 ORDER BY pr.created_at DESC`, devID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ProposalWithProject, error) {
		var x ProposalWithProject
		err := row.Scan(&x.ID, &x.ProjectID, &x.DeveloperID, &x.Price, &x.Message, &x.Status, &x.CreatedAt, &x.ProjectTitle)
		return x, err
	})
}

// Accept marks one proposal accepted, rejects the other pending ones and moves
// the project to in_progress, all in one transaction. The project row is locked
// so two accepts cannot race.
func (r *Repo) Accept(ctx context.Context, proposalID, clientID string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var projectID, propStatus, projStatus string
	err = tx.QueryRow(ctx,
		`SELECT p.id::text, pr.status, p.status
		 FROM proposals pr JOIN projects p ON p.id = pr.project_id
		 WHERE pr.id = $1 AND p.client_id = $2 FOR UPDATE OF p`, proposalID, clientID).
		Scan(&projectID, &propStatus, &projStatus)
	if err != nil {
		return mapErr(err)
	}
	if propStatus != "pending" || projStatus != "open" {
		return ErrConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE proposals SET status = 'accepted' WHERE id = $1 AND status = 'pending'`, proposalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx,
		`UPDATE proposals SET status = 'rejected' WHERE project_id = $1 AND id <> $2 AND status = 'pending'`,
		projectID, proposalID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE projects SET status = 'in_progress' WHERE id = $1`, projectID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repo) Withdraw(ctx context.Context, proposalID, devID string) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE proposals SET status = 'withdrawn' WHERE id = $1 AND developer_id = $2 AND status = 'pending'`,
		proposalID, devID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
