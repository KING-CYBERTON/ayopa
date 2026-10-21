package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Post is a shop blog post. PublishedAt is set the first time the post is
// published and kept when it goes back to draft, so republishing doesn't
// move it to the top of the blog.
type Post struct {
	ID          int64
	TenantID    int64
	Title       string
	Excerpt     string
	Body        string
	Category    string
	Status      string
	PublishedAt *time.Time
	UpdatedAt   time.Time
}

const postColumns = `id, tenant_id, title, excerpt, body, category, status, published_at, updated_at`

func scanPost(row pgx.Row) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.TenantID, &p.Title, &p.Excerpt, &p.Body, &p.Category,
		&p.Status, &p.PublishedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Post{}, ErrNotFound
	}
	return p, err
}

// ListPosts returns a shop's posts, newest first. Drafts are included only
// when publishedOnly is false.
func (s *Store) ListPosts(ctx context.Context, tenantID int64, publishedOnly bool) ([]Post, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+postColumns+` FROM posts
		  WHERE tenant_id = $1 AND (NOT $2 OR status = 'published')
		  ORDER BY published_at DESC NULLS FIRST, id DESC`,
		tenantID, publishedOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) HasPublishedPosts(ctx context.Context, tenantID int64) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM posts WHERE tenant_id = $1 AND status = 'published')`,
		tenantID).Scan(&ok)
	return ok, err
}

func (s *Store) GetPost(ctx context.Context, tenantID, id int64, publishedOnly bool) (Post, error) {
	return scanPost(s.pool.QueryRow(ctx,
		`SELECT `+postColumns+` FROM posts
		  WHERE tenant_id = $1 AND id = $2 AND (NOT $3 OR status = 'published')`,
		tenantID, id, publishedOnly))
}

func (s *Store) CreatePost(ctx context.Context, tenantID int64, p Post) (Post, error) {
	return scanPost(s.pool.QueryRow(ctx,
		`INSERT INTO posts (tenant_id, title, excerpt, body, category, status, published_at)
		 VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $6 = 'published' THEN now() END)
		 RETURNING `+postColumns,
		tenantID, p.Title, p.Excerpt, p.Body, p.Category, p.Status))
}

func (s *Store) UpdatePost(ctx context.Context, tenantID int64, p Post) (Post, error) {
	return scanPost(s.pool.QueryRow(ctx,
		`UPDATE posts SET title = $3, excerpt = $4, body = $5, category = $6, status = $7,
		        published_at = CASE WHEN $7 = 'published' THEN COALESCE(published_at, now()) ELSE published_at END,
		        updated_at = now()
		  WHERE tenant_id = $1 AND id = $2
		 RETURNING `+postColumns,
		tenantID, p.ID, p.Title, p.Excerpt, p.Body, p.Category, p.Status))
}

func (s *Store) DeletePost(ctx context.Context, tenantID, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM posts WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PublishedProductsByIDs returns the published products among ids, in the
// order given. Blog posts use it to show the products they link to.
func (s *Store) PublishedProductsByIDs(ctx context.Context, tenantID int64, ids []int64) ([]Product, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, p.name, p.price_minor, p.stock
		   FROM unnest($2::bigint[]) WITH ORDINALITY AS want(id, ord)
		   JOIN products p ON p.id = want.id
		  WHERE p.tenant_id = $1 AND p.status = 'published'
		  ORDER BY want.ord`,
		tenantID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Product
	for rows.Next() {
		p := Product{TenantID: tenantID}
		if err := rows.Scan(&p.ID, &p.Name, &p.PriceMinor, &p.Stock); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
