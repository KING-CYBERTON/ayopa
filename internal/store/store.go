// Package store is the only code that talks to the database. Shop lookups are
// scoped to a mall, and every query on shop data filters on a tenant ID.
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
	ErrSubdomainTaken = errors.New("subdomain already taken in this mall")
	ErrDomainTaken    = errors.New("mall domain already exists")
	ErrDomainOverlap  = errors.New("mall domain overlaps with an existing mall")
	ErrEmailTaken     = errors.New("email already registered for this shop")
)

const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

const mallLockID int64 = 7263542

type Mall struct {
	ID           int64
	Domain       string
	Name         string
	Status       string
	Reserved     []string
	Tagline      string
	Description  string
	ContactEmail string
}

type Tenant struct {
	ID        int64
	MallID    int64
	Subdomain string
	Name      string
	Status    string
}

type User struct {
	ID           int64
	TenantID     int64
	Email        string
	PasswordHash string
	Role         string
}

type Invite struct {
	Email string
	Role  string
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

// ---- malls ----

const mallColumns = `id, domain, name, status, reserved, tagline, description, contact_email`

func scanMall(row pgx.Row) (Mall, error) {
	var m Mall
	err := row.Scan(&m.ID, &m.Domain, &m.Name, &m.Status, &m.Reserved,
		&m.Tagline, &m.Description, &m.ContactEmail)
	return m, err
}

// MallsByDomains returns every mall whose domain is in the list. The router
// uses one query to test the host, its parent domain and the www-less host.
func (s *Store) MallsByDomains(ctx context.Context, domains []string) ([]Mall, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+mallColumns+` FROM malls WHERE domain = ANY($1)`, domains)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Mall
	for rows.Next() {
		m, err := scanMall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MallByDomain(ctx context.Context, domain string) (Mall, error) {
	m, err := scanMall(s.pool.QueryRow(ctx,
		`SELECT `+mallColumns+` FROM malls WHERE domain = $1`, domain))
	if errors.Is(err, pgx.ErrNoRows) {
		return Mall{}, ErrNotFound
	}
	return m, err
}

func (s *Store) ListMalls(ctx context.Context) ([]Mall, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+mallColumns+` FROM malls ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Mall
	for rows.Next() {
		m, err := scanMall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateMall rejects a domain that is a parent or child of an existing mall.
// Resolution assumes no mall domain sits inside another one.
func (s *Store) CreateMall(ctx context.Context, domain, name string, reserved []string) (Mall, error) {
	if reserved == nil {
		reserved = []string{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Mall{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize mall creation so the overlap check cannot race.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, mallLockID); err != nil {
		return Mall{}, err
	}

	var overlap bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM malls
		    WHERE domain <> $1::text
		      AND (domain LIKE ('%.' || $1::text) OR $1::text LIKE ('%.' || domain))
		 )`, domain).Scan(&overlap)
	if err != nil {
		return Mall{}, err
	}
	if overlap {
		return Mall{}, ErrDomainOverlap
	}

	m := Mall{Domain: domain, Name: name, Status: StatusActive, Reserved: reserved}
	err = tx.QueryRow(ctx,
		`INSERT INTO malls (domain, name, reserved) VALUES ($1, $2, $3) RETURNING id`,
		domain, name, reserved).Scan(&m.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Mall{}, ErrDomainTaken
		}
		return Mall{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Mall{}, err
	}
	return m, nil
}

func (s *Store) SetMallStatus(ctx context.Context, domain, status string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE malls SET status = $2 WHERE domain = $1`, domain, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetMallContent(ctx context.Context, domain, tagline, description, contact string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE malls SET tagline = $2, description = $3, contact_email = $4 WHERE domain = $1`,
		domain, tagline, description, contact)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- tenants (shops) ----

func (s *Store) TenantBySubdomain(ctx context.Context, mallID int64, subdomain string) (Tenant, error) {
	var t Tenant
	err := s.pool.QueryRow(ctx,
		`SELECT id, mall_id, subdomain, name, status
		   FROM tenants WHERE mall_id = $1 AND subdomain = $2`,
		mallID, subdomain,
	).Scan(&t.ID, &t.MallID, &t.Subdomain, &t.Name, &t.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	return t, err
}

func (s *Store) ListTenants(ctx context.Context, mallID int64, includeSuspended bool) ([]Tenant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, mall_id, subdomain, name, status
		   FROM tenants
		  WHERE mall_id = $1 AND ($2 OR status = 'active')
		  ORDER BY name, id`,
		mallID, includeSuspended)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.MallID, &t.Subdomain, &t.Name, &t.Status); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateTenantWithInvite creates the shop and its first invite in one
// transaction. Uniqueness comes from the (mall_id, subdomain) index.
func (s *Store) CreateTenantWithInvite(ctx context.Context, mallID int64, subdomain, name, email, role string, tokenHash []byte, expires time.Time) (Tenant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tenant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t := Tenant{MallID: mallID, Subdomain: subdomain, Name: name, Status: StatusActive}
	err = tx.QueryRow(ctx,
		`INSERT INTO tenants (mall_id, subdomain, name) VALUES ($1, $2, $3) RETURNING id`,
		mallID, subdomain, name).Scan(&t.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return Tenant{}, ErrSubdomainTaken
		}
		return Tenant{}, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO invites (tenant_id, email, role, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		t.ID, email, role, tokenHash, expires)
	if err != nil {
		return Tenant{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Tenant{}, err
	}
	return t, nil
}

func (s *Store) SetTenantStatus(ctx context.Context, mallID int64, subdomain, status string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenants SET status = $3 WHERE mall_id = $1 AND subdomain = $2`,
		mallID, subdomain, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- invites ----

// CreateInvite revokes earlier unused invites for the same email, so a newer
// link replaces a lost one.
func (s *Store) CreateInvite(ctx context.Context, tenantID int64, email, role string, tokenHash []byte, expires time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE invites SET used_at = now()
		  WHERE tenant_id = $1 AND lower(email) = lower($2) AND used_at IS NULL`,
		tenantID, email); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO invites (tenant_id, email, role, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		tenantID, email, role, tokenHash, expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) InviteByToken(ctx context.Context, tokenHash []byte, tenantID int64) (Invite, error) {
	var inv Invite
	err := s.pool.QueryRow(ctx,
		`SELECT email, role FROM invites
		  WHERE token_hash = $1 AND tenant_id = $2
		    AND used_at IS NULL AND expires_at > now()`,
		tokenHash, tenantID,
	).Scan(&inv.Email, &inv.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, ErrNotFound
	}
	return inv, err
}

// AcceptInvite claims the invite and creates the user in one transaction.
// The claim is a single UPDATE, so a link works exactly once even when two
// requests race.
func (s *Store) AcceptInvite(ctx context.Context, tokenHash []byte, tenantID int64, passwordHash string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var email, role string
	err = tx.QueryRow(ctx,
		`UPDATE invites SET used_at = now()
		  WHERE token_hash = $1 AND tenant_id = $2
		    AND used_at IS NULL AND expires_at > now()
		  RETURNING email, role`,
		tokenHash, tenantID).Scan(&email, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}

	var userID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO users (tenant_id, email, password_hash, role)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		tenantID, email, passwordHash, role).Scan(&userID)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrEmailTaken
		}
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Store) DeleteStaleInvites(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM invites WHERE expires_at < now() - interval '7 days'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ---- users and sessions ----

func (s *Store) UserByEmail(ctx context.Context, tenantID int64, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, email, password_hash, role
		   FROM users
		  WHERE tenant_id = $1 AND lower(email) = lower($2)`,
		tenantID, email,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role)
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

// SessionUser returns the user for a session only if it belongs to the given
// tenant and has not expired.
func (s *Store) SessionUser(ctx context.Context, tokenHash []byte, tenantID int64) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT u.id, u.tenant_id, u.email, u.password_hash, u.role
		   FROM sessions s
		   JOIN users u ON u.id = s.user_id
		  WHERE s.token_hash = $1
		    AND s.tenant_id = $2
		    AND u.tenant_id = $2
		    AND s.expires_at > now()`,
		tokenHash, tenantID,
	).Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role)
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

// ---- notes ----

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
