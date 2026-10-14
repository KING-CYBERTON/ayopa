package catalog_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/KING-CYBERTON/ayopa/internal/catalog"
)

func parse(qs, currency string) catalog.ListQuery {
	v, _ := url.ParseQuery(qs)
	return catalog.ParseListQuery(v, currency)
}

func TestParseListQueryDefaults(t *testing.T) {
	q := parse("", "KES")
	if q.Sort != catalog.SortNewest {
		t.Errorf("default sort = %q, want newest", q.Sort)
	}
	if q.Page != 1 {
		t.Errorf("default page = %d, want 1", q.Page)
	}
	if q.MinPrice != -1 || q.MaxPrice != -1 {
		t.Error("default prices should be -1 (unset)")
	}
	if q.InStock {
		t.Error("default InStock should be false")
	}
	if q.IsFiltered() {
		t.Error("empty query should not be filtered")
	}
}

func TestParseListQuerySort(t *testing.T) {
	for _, s := range []string{"newest", "price_asc", "price_desc", "name_asc"} {
		q := parse("sort="+s, "KES")
		if string(q.Sort) != s {
			t.Errorf("sort %q: got %q", s, q.Sort)
		}
	}
	q := parse("sort=invalid", "KES")
	if q.Sort != catalog.SortNewest {
		t.Error("invalid sort should fall back to newest")
	}
}

func TestParseListQuerySearchTruncated(t *testing.T) {
	long := strings.Repeat("a", 90)
	q := parse("q="+long, "KES")
	if catalog.RuneLen(q.Q) != 80 {
		t.Errorf("q rune len = %d, want 80", catalog.RuneLen(q.Q))
	}
}

func TestParseListQuerySearchEmpty(t *testing.T) {
	q := parse("q=   ", "KES")
	if q.Q != "" {
		t.Errorf("whitespace-only q should be empty, got %q", q.Q)
	}
}

func TestParseListQueryPrices(t *testing.T) {
	q := parse("min=10.00&max=50.00", "KES")
	if q.MinPrice != 1000 {
		t.Errorf("min = %d, want 1000", q.MinPrice)
	}
	if q.MaxPrice != 5000 {
		t.Errorf("max = %d, want 5000", q.MaxPrice)
	}
}

func TestParseListQueryReversedPriceDiscarded(t *testing.T) {
	q := parse("min=100&max=50", "KES")
	if q.MinPrice != -1 || q.MaxPrice != -1 {
		t.Error("reversed price range should be discarded")
	}
}

func TestParseListQueryInvalidPriceIgnored(t *testing.T) {
	q := parse("min=abc&max=xyz", "KES")
	if q.MinPrice != -1 || q.MaxPrice != -1 {
		t.Error("non-numeric price should be ignored (unset)")
	}
}

func TestParseListQueryPage(t *testing.T) {
	q := parse("page=3", "KES")
	if q.Page != 3 {
		t.Errorf("page = %d, want 3", q.Page)
	}
	q = parse("page=0", "KES")
	if q.Page != 1 {
		t.Errorf("page 0 should clamp to 1, got %d", q.Page)
	}
	q = parse("page=-5", "KES")
	if q.Page != 1 {
		t.Errorf("negative page should clamp to 1, got %d", q.Page)
	}
}

func TestParseListQueryIsFiltered(t *testing.T) {
	if parse("", "KES").IsFiltered() {
		t.Error("empty is filtered")
	}
	if !parse("cat=shoes", "KES").IsFiltered() {
		t.Error("category filter should mark as filtered")
	}
	if !parse("q=hat", "KES").IsFiltered() {
		t.Error("search should mark as filtered")
	}
	if !parse("stock=1", "KES").IsFiltered() {
		t.Error("stock filter should mark as filtered")
	}
	if !parse("sort=price_asc", "KES").IsFiltered() {
		t.Error("non-default sort should mark as filtered")
	}
	if !parse("page=2", "KES").IsFiltered() {
		t.Error("page > 1 should mark as filtered")
	}
}

func TestQueryStringForPagePreservesFilters(t *testing.T) {
	q := parse("cat=shoes&q=red&sort=price_asc&stock=1", "KES")
	qs := q.QueryStringForPage(2)
	v, _ := url.ParseQuery(qs)
	if v.Get("cat") != "shoes" {
		t.Error("cat not preserved")
	}
	if v.Get("q") != "red" {
		t.Error("q not preserved")
	}
	if v.Get("sort") != "price_asc" {
		t.Error("sort not preserved")
	}
	if v.Get("stock") != "1" {
		t.Error("stock not preserved")
	}
	if v.Get("page") != "2" {
		t.Errorf("page = %q, want 2", v.Get("page"))
	}
}

func TestQueryStringForPageOmitsDefaultPage(t *testing.T) {
	q := parse("cat=shoes", "KES")
	qs := q.QueryStringForPage(1)
	v, _ := url.ParseQuery(qs)
	if v.Get("page") != "" {
		t.Error("page=1 should be omitted from query string")
	}
}

func TestOffset(t *testing.T) {
	q := parse("page=3", "KES")
	want := 2 * catalog.PageSize
	if q.Offset() != want {
		t.Errorf("Offset() = %d, want %d", q.Offset(), want)
	}
}

func TestEscapeLike(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"foo", "%foo%"},
		{"foo%bar", "%foo!%bar%"},
		{"_start", "%!_start%"},
		{"has!bang", "%has!!bang%"},
	}
	for _, c := range cases {
		got := catalog.EscapeLike(c.in)
		if got != c.want {
			t.Errorf("EscapeLike(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
