// Package store is the only code that talks to the database. Every query on
// tenant data takes a tenant ID and filters on it.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrSubdomainTaken = errors.New("subdomain already taken")
)

type Tenant struct {
	ID        int64
	Subdomain string
}

type User struct {
	ID           int64
	TenantID     int64
	Email        string
	PasswordHash string
}

type Note struct {
	ID        int64
	Body      string
	Author    string
	CreatedAt time.Time
}

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Store) TenantBySubdomain(ctx context.Context, subdomain string) (Tenant, error) {
	var t Tenant
	err := s.pool.QueryRow(ctx,
		`SELECT id, subdomain FROM tenants WHERE subdomain = $1`, subdomain,
	).Scan(&t.ID, &t.Subdomain)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	return t, err
}

// CreateTenantWithOwner inserts the tenant and its first user in one
// transaction. Uniqueness is enforced by the database constraint, so two
// simultaneous signups for the same name cannot both succeed.
func (s *Store) CreateTenantWithOwner(ctx context.Context, subdomain, email, passwordHash string) (tenantID, userID int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = tx.QueryRow(ctx,
		`INSERT INTO tenants (subdomain) VALUES ($1) RETURNING id`, subdomain,
	).Scan(&tenantID)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, 0, ErrSubdomainTaken
		}
		return 0, 0, err
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, password_hash) VALUES ($1, $2, $3) RETURNING id`,
		tenantID, email, passwordHash,
	).Scan(&userID)
	if err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return tenantID, userID, nil
}

func (s *Store) UserByEmail(ctx context.Context, tenantID int64, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, email, password_hash
		   FROM users
		  WHERE tenant_id = $1 AND lower(email) = lower($2)`,
		tenantID, email,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, userID, tenantID int64, expires time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, tenant_id, expires_at) VALUES ($1, $2, $3, $4)`,
		tokenHash, userID, tenantID, expires)
	return err
}

// SessionUser returns the user for a session, but only if the session
// belongs to the given tenant and has not expired. A session from one
// tenant never authenticates on another tenant's host.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte, tenantID int64) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT u.id, u.tenant_id, u.email, u.password_hash
		   FROM sessions s
		   JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = $1
		    AND s.tenant_id = $2
		    AND u.tenant_id = $2
		    AND s.expires_at > now()`,
		tokenHash, tenantID,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte, tenantID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM sessions WHERE token_hash = $1 AND tenant_id = $2`, tokenHash, tenantID)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) AddNote(ctx context.Context, tenantID, userID int64, body string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notes (tenant_id, user_id, body) VALUES ($1, $2, $3)`,
		tenantID, userID, body)
	return err
}

func (s *Store) ListNotes(ctx context.Context, tenantID int64, limit int) ([]Note, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT n.id, n.body, u.email, n.created_at
		   FROM notes n
		   JOIN users u ON u.id = n.user_id AND u.tenant_id = n.tenant_id
		  WHERE n.tenant_id = $1
		  ORDER BY n.created_at DESC, n.id DESC
		  LIMIT $2`,
		tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Body, &n.Author, &n.CreatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}
