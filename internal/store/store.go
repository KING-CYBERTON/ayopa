// Package store is the only code that talks to the database. Shop lookups are
// scoped to a mall, and every query on shop data filters on a tenant ID.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KING-CYBERTON/ayopa/internal/catalog"
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
	Style        string // landing layout: classic, editorial or directory
}

type Tenant struct {
	ID        int64
	MallID    int64
	Subdomain string
	Name      string
	Status    string
	Currency  string // default 'KES'; populated by TenantBySubdomain
	Style     string // storefront layout: classic, boutique or market; populated by TenantBySubdomain
}

// TenantProfile extends Tenant with the operator-visible shop profile fields
// added by migration 0005.  Fetched only when needed (storefront, admin settings).
type TenantProfile struct {
	Tenant
	Description  string
	Tagline      string
	ContactEmail string
	Phone        string
	Address      string
	OpeningHours string
}

// Category is a product grouping within one shop.
type Category struct {
	ID       int64
	TenantID int64
	Name     string
	Slug     string
	Position int
	Count    int // published products; populated by ListCategoriesWithCounts
}

// Product status constants.
const (
	ProductDraft     = "draft"
	ProductPublished = "published"
	ProductArchived  = "archived"
)

// Product holds catalog data for a single item.
type Product struct {
	ID           int64
	TenantID     int64
	CategoryID   *int64 // nil = uncategorised
	CategoryName string // joined from categories; empty when uncategorised
	Name         string
	Description  string
	PriceMinor   int64
	Stock        int
	Status       string
	Featured     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
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

const mallColumns = `id, domain, name, status, reserved, tagline, description, contact_email, style`

func scanMall(row pgx.Row) (Mall, error) {
	var m Mall
	err := row.Scan(&m.ID, &m.Domain, &m.Name, &m.Status, &m.Reserved,
		&m.Tagline, &m.Description, &m.ContactEmail, &m.Style)
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

func (s *Store) SetMallStyle(ctx context.Context, domain, style string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE malls SET style = $2 WHERE domain = $1`, domain, style)
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
		`SELECT id, mall_id, subdomain, name, status, currency, style
		   FROM tenants WHERE mall_id = $1 AND subdomain = $2`,
		mallID, subdomain,
	).Scan(&t.ID, &t.MallID, &t.Subdomain, &t.Name, &t.Status, &t.Currency, &t.Style)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	return t, err
}

// TenantProfileByID returns the full shop profile for a tenant.
func (s *Store) TenantProfileByID(ctx context.Context, tenantID int64) (TenantProfile, error) {
	var p TenantProfile
	err := s.pool.QueryRow(ctx,
		`SELECT id, mall_id, subdomain, name, status, currency,
		        description, tagline, contact_email, phone, address, opening_hours
		   FROM tenants WHERE id = $1`, tenantID,
	).Scan(&p.ID, &p.MallID, &p.Subdomain, &p.Name, &p.Status, &p.Currency,
		&p.Description, &p.Tagline, &p.ContactEmail, &p.Phone, &p.Address, &p.OpeningHours)
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantProfile{}, ErrNotFound
	}
	return p, err
}

// UpdateShopProfile saves the editable shop profile fields.
func (s *Store) UpdateShopProfile(ctx context.Context, tenantID int64, p TenantProfile) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenants
		    SET description   = $2,
		        tagline       = $3,
		        contact_email = $4,
		        phone         = $5,
		        address       = $6,
		        opening_hours = $7,
		        currency      = $8
		  WHERE id = $1`,
		tenantID, p.Description, p.Tagline, p.ContactEmail,
		p.Phone, p.Address, p.OpeningHours, p.Currency)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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

func (s *Store) SetTenantStyle(ctx context.Context, mallID int64, subdomain, style string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenants SET style = $3 WHERE mall_id = $1 AND subdomain = $2`,
		mallID, subdomain, style)
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

// ---- categories ----

// Slugify converts a name to a URL-safe slug.
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prev := '-'
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prev = r
		default:
			if prev != '-' {
				b.WriteByte('-')
			}
			prev = '-'
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Store) ListCategories(ctx context.Context, tenantID int64) ([]Category, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, slug, position
		   FROM categories WHERE tenant_id = $1 ORDER BY position, id`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Slug, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListCategoriesWithCounts returns categories with a count of published products.
func (s *Store) ListCategoriesWithCounts(ctx context.Context, tenantID int64) ([]Category, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.id, c.tenant_id, c.name, c.slug, c.position,
		        COUNT(p.id) FILTER (WHERE p.status = 'published') AS cnt
		   FROM categories c
		   LEFT JOIN products p ON p.category_id = c.id AND p.tenant_id = c.tenant_id
		  WHERE c.tenant_id = $1
		  GROUP BY c.id
		  ORDER BY c.position, c.id`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Slug, &c.Position, &c.Count); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

var ErrSlugTaken = errors.New("a category with that slug already exists")

func (s *Store) CreateCategory(ctx context.Context, tenantID int64, name string) (Category, error) {
	slug := Slugify(name)
	if slug == "" {
		return Category{}, fmt.Errorf("category name produces empty slug")
	}
	var c Category
	err := s.pool.QueryRow(ctx,
		`INSERT INTO categories (tenant_id, name, slug, position)
		 VALUES ($1, $2, $3,
		   (SELECT COALESCE(MAX(position), 0) + 1 FROM categories WHERE tenant_id = $1))
		 RETURNING id, tenant_id, name, slug, position`,
		tenantID, name, slug,
	).Scan(&c.ID, &c.TenantID, &c.Name, &c.Slug, &c.Position)
	if err != nil {
		if isUniqueViolation(err) {
			return Category{}, ErrSlugTaken
		}
		return Category{}, err
	}
	return c, nil
}

func (s *Store) RenameCategory(ctx context.Context, tenantID, id int64, name string) error {
	slug := Slugify(name)
	if slug == "" {
		return fmt.Errorf("category name produces empty slug")
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE categories SET name = $3, slug = $4 WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, name, slug)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrSlugTaken
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCategory detaches all products from this category (sets category_id = NULL)
// and then deletes the category, all in one transaction.
func (s *Store) DeleteCategory(ctx context.Context, tenantID, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE products SET category_id = NULL WHERE tenant_id = $1 AND category_id = $2`,
		tenantID, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx,
		`DELETE FROM categories WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// ---- products ----

const productColumns = `p.id, p.tenant_id, p.category_id,
    COALESCE(c.name, ''), p.name, p.description,
    p.price_minor, p.stock, p.status, p.featured, p.created_at, p.updated_at`

func scanProduct(row pgx.Row) (Product, error) {
	var p Product
	err := row.Scan(
		&p.ID, &p.TenantID, &p.CategoryID, &p.CategoryName,
		&p.Name, &p.Description, &p.PriceMinor, &p.Stock,
		&p.Status, &p.Featured, &p.CreatedAt, &p.UpdatedAt,
	)
	return p, err
}

// sortClauses maps validated sort keys to their ORDER BY fragment.
var sortClauses = map[catalog.Sort]string{
	catalog.SortNewest:    "p.created_at DESC, p.id DESC",
	catalog.SortPriceAsc:  "p.price_minor ASC, p.id ASC",
	catalog.SortPriceDesc: "p.price_minor DESC, p.id DESC",
	catalog.SortNameAsc:   "p.name ASC, p.id ASC",
}

// ListProducts returns published products matching the query plus the total count.
func (s *Store) ListProducts(ctx context.Context, tenantID int64, q catalog.ListQuery) ([]Product, int, error) {
	conds := []string{"p.tenant_id = $1", "p.status = 'published'"}
	args := []any{tenantID}

	// Resolve category slug to an id first.
	var catID int64
	if q.Category != "" {
		err := s.pool.QueryRow(ctx,
			`SELECT id FROM categories WHERE tenant_id = $1 AND slug = $2`,
			tenantID, q.Category).Scan(&catID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, nil // unknown slug = empty result, not an error
		}
		if err != nil {
			return nil, 0, err
		}
		args = append(args, catID)
		conds = append(conds, fmt.Sprintf("p.category_id = $%d", len(args)))
	}

	if q.Q != "" {
		args = append(args, catalog.EscapeLike(q.Q))
		n := len(args)
		conds = append(conds,
			fmt.Sprintf("(p.name ILIKE $%d ESCAPE '!' OR p.description ILIKE $%d ESCAPE '!')", n, n))
	}
	if q.MinPrice >= 0 {
		args = append(args, q.MinPrice)
		conds = append(conds, fmt.Sprintf("p.price_minor >= $%d", len(args)))
	}
	if q.MaxPrice >= 0 {
		args = append(args, q.MaxPrice)
		conds = append(conds, fmt.Sprintf("p.price_minor <= $%d", len(args)))
	}
	if q.InStock {
		conds = append(conds, "p.stock > 0")
	}

	where := strings.Join(conds, " AND ")
	order := sortClauses[q.Sort]
	if order == "" {
		order = sortClauses[catalog.SortNewest]
	}

	var total int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM products p WHERE `+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	args = append(args, catalog.PageSize, q.Offset())
	rows, err := s.pool.Query(ctx,
		`SELECT `+productColumns+`
		   FROM products p
		   LEFT JOIN categories c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
		  WHERE `+where+`
		  ORDER BY `+order+`
		  LIMIT $`+fmt.Sprintf("%d", len(args)-1)+` OFFSET $`+fmt.Sprintf("%d", len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// FeaturedProducts returns up to limit published, featured products ordered newest first.
func (s *Store) FeaturedProducts(ctx context.Context, tenantID int64, limit int) ([]Product, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+productColumns+`
		   FROM products p
		   LEFT JOIN categories c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
		  WHERE p.tenant_id = $1 AND p.status = 'published' AND p.featured = true
		  ORDER BY p.created_at DESC, p.id DESC
		  LIMIT $2`,
		tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPublishedProduct returns a published product visible to shoppers.
func (s *Store) GetPublishedProduct(ctx context.Context, tenantID, id int64) (Product, error) {
	p, err := scanProduct(s.pool.QueryRow(ctx,
		`SELECT `+productColumns+`
		   FROM products p
		   LEFT JOIN categories c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
		  WHERE p.tenant_id = $1 AND p.id = $2 AND p.status = 'published'`,
		tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	return p, err
}

// GetProductAdmin returns any product regardless of status (for admin editing).
func (s *Store) GetProductAdmin(ctx context.Context, tenantID, id int64) (Product, error) {
	p, err := scanProduct(s.pool.QueryRow(ctx,
		`SELECT `+productColumns+`
		   FROM products p
		   LEFT JOIN categories c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
		  WHERE p.tenant_id = $1 AND p.id = $2`,
		tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	return p, err
}

// ListProductsAdmin returns all products with an optional status filter and pagination.
func (s *Store) ListProductsAdmin(ctx context.Context, tenantID int64, statusFilter string, page int) ([]Product, int, error) {
	conds := []string{"p.tenant_id = $1"}
	args := []any{tenantID}
	if statusFilter != "" {
		args = append(args, statusFilter)
		conds = append(conds, fmt.Sprintf("p.status = $%d", len(args)))
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM products p WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * catalog.PageSize
	args = append(args, catalog.PageSize, offset)
	rows, err := s.pool.Query(ctx,
		`SELECT `+productColumns+`
		   FROM products p
		   LEFT JOIN categories c ON c.id = p.category_id AND c.tenant_id = p.tenant_id
		  WHERE `+where+`
		  ORDER BY p.created_at DESC, p.id DESC
		  LIMIT $`+fmt.Sprintf("%d", len(args)-1)+` OFFSET $`+fmt.Sprintf("%d", len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, p)
	}
	return out, total, rows.Err()
}

// CreateProduct inserts a new product with status 'draft'.
func (s *Store) CreateProduct(ctx context.Context, tenantID int64, name, description string, price int64, stock int, catID *int64) (Product, error) {
	var p Product
	err := s.pool.QueryRow(ctx,
		`INSERT INTO products (tenant_id, category_id, name, description, price_minor, stock)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, tenant_id, category_id, ''::text, name, description,
		           price_minor, stock, status, featured, created_at, updated_at`,
		tenantID, catID, name, description, price, stock,
	).Scan(&p.ID, &p.TenantID, &p.CategoryID, &p.CategoryName,
		&p.Name, &p.Description, &p.PriceMinor, &p.Stock,
		&p.Status, &p.Featured, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// UpdateProduct edits the mutable fields of a product.
func (s *Store) UpdateProduct(ctx context.Context, tenantID, id int64, name, description string, price int64, stock int, catID *int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products
		    SET name        = $3,
		        description = $4,
		        price_minor = $5,
		        stock       = $6,
		        category_id = $7,
		        updated_at  = now()
		  WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, name, description, price, stock, catID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

var ErrInvalidProductStatus = errors.New("invalid product status")

// SetProductStatus updates status, subject to valid transitions.
func (s *Store) SetProductStatus(ctx context.Context, tenantID, id int64, status string) error {
	if status != ProductDraft && status != ProductPublished && status != ProductArchived {
		return ErrInvalidProductStatus
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET status = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetProductFeatured toggles the featured flag.
func (s *Store) SetProductFeatured(ctx context.Context, tenantID, id int64, featured bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE products SET featured = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2`,
		tenantID, id, featured)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
