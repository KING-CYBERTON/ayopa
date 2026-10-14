# ADR-021: Shop Storefront with Product Catalog

**Date:** 2026-10-14  
**Status:** Accepted

## Context

Shops needed a public-facing catalog — product listing with filters, a product detail page, and shop-owner admin tools to manage categories, products, and shop profile settings.

## Decisions

### Template architecture

All templates moved from a single Go string constant (`templates.go`) to individual files in `internal/web/templates/*.html` embedded via `//go:embed`. A `template.FuncMap` provides: `formatMoney`, `formatPrice`, `placeholderColor`, `initial`, `queryPage`, `querySort`, `paginate`, `catSelected`, `adminPageQS`, `currencies`, `add`, `sub`. Storefront pages use a distinct `shop_head` partial with inline CSS; admin pages use the existing `head`/`foot` admin partials. No external resources are loaded — all CSS is inline, satisfying the existing CSP (`default-src 'none'; style-src 'unsafe-inline'`).

### Money representation

Prices are stored as `BIGINT` minor units (e.g. KES 99.95 → 9995). The `internal/money` package converts between decimal strings and minor units with no floating-point arithmetic. Currency exponents are looked up by ISO 4217 code; unknown currencies default to 2 decimal places.

### Catalog query parsing

`internal/catalog.ParseListQuery` validates all filter parameters from URL query strings: category slug (passed through verbatim for store lookup), search term (trimmed, max 80 runes), min/max price (parsed via `money.Parse`; reversed ranges discarded), in-stock flag, sort order (whitelisted), and page (clamped to ≥ 1). User input is never interpolated into SQL.

### ILIKE search escaping

PostgreSQL ILIKE with backslash as escape character is unreliable with `standard_conforming_strings`. The `EscapeLike` function uses `!` as the escape character (`!` → `!!`, `%` → `!%`, `_` → `!_`), and SQL uses `ILIKE $N ESCAPE '!'`.

### Dynamic SQL for product listing

`ListProducts` builds a parameterized WHERE clause from validated `ListQuery` fields. Each condition appends to a `[]string` slice; each value to `[]any`. Parameter numbers are tracked with `fmt.Sprintf("$%d", len(args))`. Sort order is selected from a whitelist map (`sortClauses`) keyed on `catalog.Sort`. The tenant always comes from the request context, never from query parameters.

### Composite FK and category deletion

Products reference categories via a composite FK `(tenant_id, category_id) → categories(tenant_id, id)` to guarantee cross-shop isolation at the database level. PostgreSQL cannot set only `category_id = NULL` on a composite FK with `ON DELETE SET NULL`, so category deletion uses an application-level transaction: first `UPDATE products SET category_id = NULL WHERE tenant_id = $1 AND category_id = $2`, then `DELETE FROM categories`.

### Tenant struct split

`Tenant` (minimal, in every route context, includes `Currency`) and `TenantProfile` (embeds `Tenant`, adds profile fields) are fetched separately: `TenantBySubdomain` is called on every request; `TenantProfileByID` only when the storefront or admin settings page is rendered.

### No-script, no-image placeholders

Product thumbnails use CSS background-color derived from product ID (`placeholderColor`) with a single initial letter (`initial`). No `<img>` tags, no external requests. This is consistent with the CSP.

### Pagination noindex

Pages with any active filter or page > 1 set `<meta name="robots" content="noindex,follow">` to avoid duplicate content penalties.

### Admin roles

Settings (currency, tagline, profile) are restricted to `role = owner`. Category and product management is available to all authenticated users of the shop.

## Consequences

- Adding image uploads in a future PR requires a new CSP directive and a new migration for image URLs.
- The money package currently supports 8 currencies. Adding more requires only a map entry and a migration to extend the CHECK constraint.
- Checkout (PR 2) can trust `PriceMinor` from the database; it must never accept prices from the client.
