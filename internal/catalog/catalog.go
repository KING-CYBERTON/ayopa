// Package catalog defines the validated query parameters for the product
// listing page. It is pure (no database access) so it can be tested cheaply.
package catalog

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/KING-CYBERTON/ayopa/internal/money"
)

// Sort is an enumeration of the allowed product sort orders.
type Sort string

const (
	SortNewest    Sort = "newest"
	SortPriceAsc  Sort = "price_asc"
	SortPriceDesc Sort = "price_desc"
	SortNameAsc   Sort = "name_asc"
)

// PageSize is the fixed number of products per page.
const PageSize = 12

var validSorts = map[Sort]bool{
	SortNewest: true, SortPriceAsc: true,
	SortPriceDesc: true, SortNameAsc: true,
}

// ListQuery holds the parsed and validated parameters from a product listing
// URL. All values are safe to use in database queries.
type ListQuery struct {
	Category string // category slug, "" = all categories
	Q        string // search term, trimmed, max 80 runes, "" = no search
	MinPrice int64  // minor units; -1 means unset
	MaxPrice int64  // minor units; -1 means unset
	InStock  bool
	Sort     Sort
	Page     int // 1-based, always >= 1

	currency string
	exp      int
}

// ParseListQuery validates and normalises filter parameters from URL query
// values. currency selects the price parsing precision (e.g. "KES" → 2 dp).
func ParseListQuery(v url.Values, currency string) ListQuery {
	exp := money.ExponentFor(currency)
	q := ListQuery{
		Sort:     SortNewest,
		Page:     1,
		MinPrice: -1,
		MaxPrice: -1,
		currency: currency,
		exp:      exp,
	}

	if cat := strings.TrimSpace(v.Get("cat")); cat != "" {
		q.Category = cat
	}

	if raw := strings.TrimSpace(v.Get("q")); raw != "" {
		runes := []rune(raw)
		if len(runes) > 80 {
			runes = runes[:80]
		}
		q.Q = string(runes)
	}

	if s := Sort(v.Get("sort")); validSorts[s] {
		q.Sort = s
	}

	if v.Get("stock") == "1" {
		q.InStock = true
	}

	if minStr := v.Get("min"); minStr != "" {
		if min, err := money.Parse(minStr, exp); err == nil {
			q.MinPrice = min
		}
	}
	if maxStr := v.Get("max"); maxStr != "" {
		if max, err := money.Parse(maxStr, exp); err == nil {
			q.MaxPrice = max
		}
	}
	// silently discard a reversed range
	if q.MinPrice >= 0 && q.MaxPrice >= 0 && q.MinPrice > q.MaxPrice {
		q.MinPrice = -1
		q.MaxPrice = -1
	}

	if pageStr := v.Get("page"); pageStr != "" {
		var p int
		fmt.Sscan(pageStr, &p)
		if p >= 1 {
			q.Page = p
		}
	}

	return q
}

// IsFiltered reports whether any non-default filter or sort is active, or the
// page is greater than 1.  Used to decide whether to add noindex.
func (q ListQuery) IsFiltered() bool {
	return q.Category != "" || q.Q != "" ||
		q.MinPrice >= 0 || q.MaxPrice >= 0 ||
		q.InStock || q.Sort != SortNewest || q.Page > 1
}

// QueryStringForPage returns the URL query string preserving all current
// filters but replacing the page number.
func (q ListQuery) QueryStringForPage(page int) string {
	v := url.Values{}
	if q.Category != "" {
		v.Set("cat", q.Category)
	}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	if q.MinPrice >= 0 {
		v.Set("min", money.Format(q.MinPrice, q.exp))
	}
	if q.MaxPrice >= 0 {
		v.Set("max", money.Format(q.MaxPrice, q.exp))
	}
	if q.InStock {
		v.Set("stock", "1")
	}
	if q.Sort != SortNewest {
		v.Set("sort", string(q.Sort))
	}
	if page > 1 {
		v.Set("page", fmt.Sprintf("%d", page))
	}
	return v.Encode()
}

// MinPriceDisplay returns the min price as a decimal string for the form
// input value, or "" when unset.
func (q ListQuery) MinPriceDisplay() string {
	if q.MinPrice < 0 {
		return ""
	}
	return money.Format(q.MinPrice, q.exp)
}

// MaxPriceDisplay returns the max price as a decimal string for the form
// input value, or "" when unset.
func (q ListQuery) MaxPriceDisplay() string {
	if q.MaxPrice < 0 {
		return ""
	}
	return money.Format(q.MaxPrice, q.exp)
}

// Offset returns the SQL OFFSET for the current page.
func (q ListQuery) Offset() int {
	return (q.Page - 1) * PageSize
}

// EscapeLike returns the search term with ILIKE special characters (%, _, !)
// escaped using ! as the escape character, wrapped in % wildcards.
// Use with ILIKE $N ESCAPE '!'.
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, "!", "!!")
	s = strings.ReplaceAll(s, "%", "!%")
	s = strings.ReplaceAll(s, "_", "!_")
	return "%" + s + "%"
}

// ValidateQ is exported for tests; it mirrors what ParseListQuery does.
func ValidateQ(raw string) string {
	raw = strings.TrimSpace(raw)
	runes := []rune(raw)
	if len(runes) > 80 {
		runes = runes[:80]
	}
	return string(runes)
}

// RuneLen returns the rune count of s (used in tests).
func RuneLen(s string) int {
	return utf8.RuneCountInString(s)
}
