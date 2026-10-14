package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/KING-CYBERTON/ayopa/internal/catalog"
	"github.com/KING-CYBERTON/ayopa/internal/store"
)

// validCurrencies is the set of currencies accepted in shop settings.
var validCurrencies = map[string]bool{
	"KES": true, "UGX": true, "TZS": true, "RWF": true,
	"ETB": true, "NGN": true, "ZAR": true, "USD": true,
}

func (s *Server) showSettings(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	if u.Role != "owner" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	profile, err := s.store.TenantProfileByID(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, "load settings", err)
		return
	}
	pg := s.page(r, "Settings — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Profile = profile
	s.render(w, http.StatusOK, "admin_settings", pg)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if u.Role != "owner" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	profile, err := s.store.TenantProfileByID(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, "load settings", err)
		return
	}
	pg := s.page(r, "Settings — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role

	description := strings.TrimSpace(r.PostFormValue("description"))
	tagline := strings.TrimSpace(r.PostFormValue("tagline"))
	contactEmail := strings.TrimSpace(r.PostFormValue("contact_email"))
	phone := strings.TrimSpace(r.PostFormValue("phone"))
	address := strings.TrimSpace(r.PostFormValue("address"))
	openingHours := strings.TrimSpace(r.PostFormValue("opening_hours"))
	currency := r.PostFormValue("currency")

	switch {
	case utf8.RuneCountInString(tagline) > 140:
		pg.Error = "Tagline must be 140 characters or fewer."
	case utf8.RuneCountInString(description) > 600:
		pg.Error = "Description must be 600 characters or fewer."
	case utf8.RuneCountInString(contactEmail) > 254:
		pg.Error = "Email address is too long."
	case utf8.RuneCountInString(phone) > 30:
		pg.Error = "Phone must be 30 characters or fewer."
	case utf8.RuneCountInString(address) > 300:
		pg.Error = "Address must be 300 characters or fewer."
	case utf8.RuneCountInString(openingHours) > 300:
		pg.Error = "Opening hours must be 300 characters or fewer."
	case !validCurrencies[currency]:
		pg.Error = "Invalid currency."
	}
	if pg.Error != "" {
		profile.Tagline = tagline
		profile.Description = description
		profile.ContactEmail = contactEmail
		profile.Phone = phone
		profile.Address = address
		profile.OpeningHours = openingHours
		profile.Currency = currency
		pg.Profile = profile
		s.render(w, http.StatusBadRequest, "admin_settings", pg)
		return
	}

	updated := profile
	updated.Tagline = tagline
	updated.Description = description
	updated.ContactEmail = contactEmail
	updated.Phone = phone
	updated.Address = address
	updated.OpeningHours = openingHours
	updated.Currency = currency

	if err := s.store.UpdateShopProfile(r.Context(), t.ID, updated); err != nil {
		s.serverError(w, "update settings", err)
		return
	}
	http.Redirect(w, r, "/admin/settings?saved=1", http.StatusSeeOther)
}

func (s *Server) showCategories(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	cats, err := s.store.ListCategoriesWithCounts(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, "list categories", err)
		return
	}
	pg := s.page(r, "Categories — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Categories = cats
	if r.URL.Query().Get("saved") == "1" {
		pg.Notice = "Saved."
	}
	s.render(w, http.StatusOK, "admin_categories", pg)
}

func (s *Server) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if n := utf8.RuneCountInString(name); n < 1 || n > 60 {
		cats, _ := s.store.ListCategoriesWithCounts(r.Context(), t.ID)
		pg := s.page(r, "Categories — "+t.Name)
		pg.UserEmail = u.Email
		pg.Role = u.Role
		pg.Categories = cats
		pg.Error = "Category name must be 1 to 60 characters."
		s.render(w, http.StatusBadRequest, "admin_categories", pg)
		return
	}
	if _, err := s.store.CreateCategory(r.Context(), t.ID, name); err != nil {
		if errors.Is(err, store.ErrSlugTaken) {
			cats, _ := s.store.ListCategoriesWithCounts(r.Context(), t.ID)
			pg := s.page(r, "Categories — "+t.Name)
			pg.UserEmail = u.Email
			pg.Role = u.Role
			pg.Categories = cats
			pg.Error = "A category with that name already exists."
			s.render(w, http.StatusConflict, "admin_categories", pg)
			return
		}
		s.serverError(w, "create category", err)
		return
	}
	http.Redirect(w, r, "/admin/categories?saved=1", http.StatusSeeOther)
}

func (s *Server) handleRenameCategory(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	_, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if n := utf8.RuneCountInString(name); n < 1 || n > 60 {
		http.Error(w, "name must be 1 to 60 characters", http.StatusBadRequest)
		return
	}
	if err := s.store.RenameCategory(r.Context(), t.ID, id, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, store.ErrSlugTaken) {
			http.Error(w, "a category with that name already exists", http.StatusConflict)
			return
		}
		s.serverError(w, "rename category", err)
		return
	}
	http.Redirect(w, r, "/admin/categories?saved=1", http.StatusSeeOther)
}

func (s *Server) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	_, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteCategory(r.Context(), t.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "delete category", err)
		return
	}
	http.Redirect(w, r, "/admin/categories", http.StatusSeeOther)
}

func (s *Server) showProducts(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	if statusFilter != "" &&
		statusFilter != store.ProductDraft &&
		statusFilter != store.ProductPublished &&
		statusFilter != store.ProductArchived {
		statusFilter = ""
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n >= 1 {
			page = n
		}
	}

	products, total, err := s.store.ListProductsAdmin(r.Context(), t.ID, statusFilter, page)
	if err != nil {
		s.serverError(w, "list products admin", err)
		return
	}
	lastPage := 1
	if total > 0 {
		lastPage = (total + catalog.PageSize - 1) / catalog.PageSize
	}

	pg := s.page(r, "Products — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Currency = t.Currency
	pg.AdminProducts = products
	pg.AdminTotal = total
	pg.AdminPage = page
	pg.AdminLastPage = lastPage
	pg.AdminStatus = statusFilter
	if r.URL.Query().Get("saved") == "1" {
		pg.Notice = "Saved."
	}
	s.render(w, http.StatusOK, "admin_products", pg)
}

func (s *Server) showNewProduct(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	cats, err := s.store.ListCategories(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, "list categories", err)
		return
	}
	pg := s.page(r, "New Product — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Currency = t.Currency
	pg.FormCats = cats
	s.render(w, http.StatusOK, "admin_product_form", pg)
}

func (s *Server) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	cats, _ := s.store.ListCategories(r.Context(), t.ID)
	pg := s.page(r, "New Product — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Currency = t.Currency
	pg.FormCats = cats

	name := strings.TrimSpace(r.PostFormValue("name"))
	description := strings.TrimSpace(r.PostFormValue("description"))
	priceStr := r.PostFormValue("price")
	stockStr := r.PostFormValue("stock")

	formProduct := store.Product{Name: name, Description: description}
	pg.FormProduct = formProduct

	switch {
	case utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120:
		pg.Error = "Name must be 1 to 120 characters."
	case utf8.RuneCountInString(description) > 2000:
		pg.Error = "Description must be 2000 characters or fewer."
	}
	if pg.Error != "" {
		s.render(w, http.StatusBadRequest, "admin_product_form", pg)
		return
	}

	price, err := parseMoney(priceStr, t.Currency)
	if err != nil {
		pg.Error = fmt.Sprintf("Invalid price: %s", err)
		s.render(w, http.StatusBadRequest, "admin_product_form", pg)
		return
	}

	stock, err := strconv.Atoi(strings.TrimSpace(stockStr))
	if err != nil || stock < 0 {
		pg.Error = "Stock must be a non-negative whole number."
		s.render(w, http.StatusBadRequest, "admin_product_form", pg)
		return
	}

	var catID *int64
	if catStr := strings.TrimSpace(r.PostFormValue("category_id")); catStr != "" {
		id, err := strconv.ParseInt(catStr, 10, 64)
		if err == nil && id > 0 {
			catID = &id
		}
	}

	product, err := s.store.CreateProduct(r.Context(), t.ID, name, description, price, stock, catID)
	if err != nil {
		s.serverError(w, "create product", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/products/%d", product.ID), http.StatusSeeOther)
}

func (s *Server) showEditProduct(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	product, err := s.store.GetProductAdmin(r.Context(), t.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get product", err)
		return
	}
	cats, err := s.store.ListCategories(r.Context(), t.ID)
	if err != nil {
		s.serverError(w, "list categories", err)
		return
	}
	pg := s.page(r, "Edit: "+product.Name+" — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Currency = t.Currency
	pg.FormProduct = product
	pg.FormCats = cats
	pg.IsEdit = true
	if r.URL.Query().Get("saved") == "1" {
		pg.Notice = "Saved."
	}
	s.render(w, http.StatusOK, "admin_product_form", pg)
}

func (s *Server) handleUpdateProduct(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	product, err := s.store.GetProductAdmin(r.Context(), t.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get product", err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	cats, _ := s.store.ListCategories(r.Context(), t.ID)
	pg := s.page(r, "Edit: "+product.Name+" — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Currency = t.Currency
	pg.FormProduct = product
	pg.FormCats = cats
	pg.IsEdit = true

	name := strings.TrimSpace(r.PostFormValue("name"))
	description := strings.TrimSpace(r.PostFormValue("description"))
	priceStr := r.PostFormValue("price")
	stockStr := r.PostFormValue("stock")

	switch {
	case utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120:
		pg.Error = "Name must be 1 to 120 characters."
	case utf8.RuneCountInString(description) > 2000:
		pg.Error = "Description must be 2000 characters or fewer."
	}
	if pg.Error != "" {
		s.render(w, http.StatusBadRequest, "admin_product_form", pg)
		return
	}

	price, err := parseMoney(priceStr, t.Currency)
	if err != nil {
		pg.Error = fmt.Sprintf("Invalid price: %s", err)
		s.render(w, http.StatusBadRequest, "admin_product_form", pg)
		return
	}

	stock, err := strconv.Atoi(strings.TrimSpace(stockStr))
	if err != nil || stock < 0 {
		pg.Error = "Stock must be a non-negative whole number."
		s.render(w, http.StatusBadRequest, "admin_product_form", pg)
		return
	}

	var catID *int64
	if catStr := strings.TrimSpace(r.PostFormValue("category_id")); catStr != "" {
		cid, err := strconv.ParseInt(catStr, 10, 64)
		if err == nil && cid > 0 {
			catID = &cid
		}
	}

	if err := s.store.UpdateProduct(r.Context(), t.ID, id, name, description, price, stock, catID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "update product", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/products/%d?saved=1", id), http.StatusSeeOther)
}

func (s *Server) handleSetProductStatus(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	_, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	status := r.PostFormValue("status")
	if err := s.store.SetProductStatus(r.Context(), t.ID, id, status); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, store.ErrInvalidProductStatus) {
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
		s.serverError(w, "set product status", err)
		return
	}
	back := r.Referer()
	if back == "" {
		back = "/admin/products"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) handleSetProductFeatured(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	_, ok := s.currentUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	featured := r.PostFormValue("featured") == "1"
	if err := s.store.SetProductFeatured(r.Context(), t.ID, id, featured); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, "set product featured", err)
		return
	}
	back := r.Referer()
	if back == "" {
		back = "/admin/products"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// parseID extracts and validates a positive int64 from a path value.
func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id %q", s)
	}
	return id, nil
}
