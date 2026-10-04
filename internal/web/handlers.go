package web

import (
	"bytes"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KING-CYBERTON/ayopa/internal/auth"
	"github.com/KING-CYBERTON/ayopa/internal/store"
	"github.com/KING-CYBERTON/ayopa/internal/tenant"
)

type pageData struct {
	Title      string
	Error      string
	Notice     string
	BaseDomain string
	Subdomain  string
	Email      string
	Tenant     string
	UserEmail  string
	Notes      []store.Note
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data pageData) {
	data.BaseDomain = s.cfg.BaseDomain
	var buf bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Error("render", "template", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func validEmail(e string) bool {
	if len(e) > 254 {
		return false
	}
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e
}

// ---- main site ----

func (s *Server) showSignup(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "signup", pageData{Title: "Create your workspace"})
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	page := pageData{Title: "Create your workspace"}

	if !s.signupRL.allow(s.clientIP(r), time.Now()) {
		page.Error = "Too many attempts. Please try again later."
		s.render(w, http.StatusTooManyRequests, "signup", page)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sub := tenant.Normalize(r.PostFormValue("subdomain"))
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	password := r.PostFormValue("password")
	page.Subdomain = sub
	page.Email = email

	fail := func(status int, msg string) {
		page.Error = msg
		s.render(w, status, "signup", page)
	}

	if err := tenant.Validate(sub); err != nil {
		if errors.Is(err, tenant.ErrReserved) {
			fail(http.StatusBadRequest, "That subdomain is reserved. Please choose another.")
		} else {
			fail(http.StatusBadRequest, "Use 3 to 63 lowercase letters, digits or hyphens, without a leading or trailing hyphen.")
		}
		return
	}
	if !validEmail(email) {
		fail(http.StatusBadRequest, "Enter a valid email address.")
		return
	}
	if n := utf8.RuneCountInString(password); n < 10 || n > 128 {
		fail(http.StatusBadRequest, "Password must be 10 to 128 characters.")
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		s.log.Error("hash password", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, _, err = s.store.CreateTenantWithOwner(r.Context(), sub, email, hash)
	if errors.Is(err, store.ErrSubdomainTaken) {
		fail(http.StatusConflict, "That subdomain is already taken.")
		return
	}
	if err != nil {
		s.log.Error("create tenant", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.log.Info("tenant created", "subdomain", sub)
	// Cookies are host-only, so signing in has to happen on the new subdomain.
	http.Redirect(w, r, s.tenantURL(sub, "/login?welcome=1"), http.StatusSeeOther)
}

// ---- tenant site ----

func (s *Server) currentUser(r *http.Request) (store.User, bool) {
	t := tenantFrom(r.Context())
	c, err := r.Cookie(s.cookieName())
	if err != nil || c.Value == "" {
		return store.User{}, false
	}
	u, err := s.store.SessionUser(r.Context(), auth.HashToken(c.Value), t.ID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			s.log.Error("session lookup", "err", err)
		}
		return store.User{}, false
	}
	return u, true
}

func (s *Server) showLogin(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	page := pageData{Title: "Log in", Tenant: t.Subdomain}
	if r.URL.Query().Get("welcome") == "1" {
		page.Notice = "Workspace created. Log in to continue."
	}
	s.render(w, http.StatusOK, "login", page)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	page := pageData{Title: "Log in", Tenant: t.Subdomain}

	if !s.loginRL.allow(s.clientIP(r), time.Now()) {
		page.Error = "Too many attempts. Please try again later."
		s.render(w, http.StatusTooManyRequests, "login", page)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	password := r.PostFormValue("password")
	page.Email = email

	u, err := s.store.UserByEmail(r.Context(), t.ID, email)
	ok := false
	switch {
	case err == nil:
		ok, err = auth.VerifyPassword(password, u.PasswordHash)
		if err != nil {
			s.log.Error("verify password", "err", err)
		}
	case errors.Is(err, store.ErrNotFound):
		auth.BurnVerify(password)
	default:
		s.log.Error("user lookup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if !ok {
		page.Error = "Invalid email or password."
		s.render(w, http.StatusUnauthorized, "login", page)
		return
	}

	token, hash, err := auth.NewToken()
	if err != nil {
		s.log.Error("new token", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.store.CreateSession(r.Context(), hash, u.ID, t.ID, time.Now().Add(s.cfg.SessionTTL)); err != nil {
		s.log.Error("create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, s.sessionCookie(token, s.cfg.SessionTTL))
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	if c, err := r.Cookie(s.cookieName()); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(c.Value), t.ID); err != nil {
			s.log.Error("delete session", "err", err)
		}
	}
	http.SetCookie(w, s.sessionCookie("", 0))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) showDashboard(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	notes, err := s.store.ListNotes(r.Context(), t.ID, 50)
	if err != nil {
		s.log.Error("list notes", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, http.StatusOK, "dashboard", pageData{
		Title:     t.Subdomain,
		Tenant:    t.Subdomain,
		UserEmail: u.Email,
		Notes:     notes,
	})
}

func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
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
	body := strings.TrimSpace(r.PostFormValue("body"))
	if n := utf8.RuneCountInString(body); n == 0 || n > 2000 {
		http.Error(w, "note must be 1 to 2000 characters", http.StatusBadRequest)
		return
	}
	// The tenant comes from the host (request context), never from the form.
	if err := s.store.AddNote(r.Context(), t.ID, u.ID, body); err != nil {
		s.log.Error("add note", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}
