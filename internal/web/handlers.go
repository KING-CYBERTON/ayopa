package web

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KING-CYBERTON/ayopa/internal/auth"
	"github.com/KING-CYBERTON/ayopa/internal/store"
)

const invalidLinkMsg = "This setup link is invalid or has expired. Ask your mall operator for a new one."

type shopLink struct {
	Name string
	URL  string
}

type pageData struct {
	Title     string
	Error     string
	Notice    string
	MallName  string
	Shop      string
	Email     string
	UserEmail string
	Role      string
	Token     string
	Shops     []shopLink
	Notes     []store.Note
}

func (s *Server) page(r *http.Request, title string) pageData {
	return pageData{
		Title:    title,
		MallName: mallFrom(r.Context()).Name,
		Shop:     tenantFrom(r.Context()).Name,
	}
}

func (s *Server) render(w http.ResponseWriter, status int, name string, data pageData) {
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

func (s *Server) serverError(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// ---- mall site ----

func (s *Server) showDirectory(w http.ResponseWriter, r *http.Request) {
	m := mallFrom(r.Context())
	shops, err := s.store.ListTenants(r.Context(), m.ID, false)
	if err != nil {
		s.serverError(w, "list shops", err)
		return
	}
	page := s.page(r, m.Name)
	for _, t := range shops {
		page.Shops = append(page.Shops, shopLink{Name: t.Name, URL: s.shopURL(m, t.Subdomain, "/")})
	}
	s.render(w, http.StatusOK, "directory", page)
}

// ---- shop site ----

func (s *Server) showStorefront(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	s.render(w, http.StatusOK, "storefront", s.page(r, t.Name))
}

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

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID, tenantID int64) error {
	token, hash, err := auth.NewToken()
	if err != nil {
		return err
	}
	if err := s.store.CreateSession(r.Context(), hash, userID, tenantID, time.Now().Add(s.cfg.SessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, s.sessionCookie(token, s.cfg.SessionTTL))
	return nil
}

func (s *Server) showLogin(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "login", s.page(r, "Log in"))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	page := s.page(r, "Log in")

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
		s.serverError(w, "user lookup", err)
		return
	}

	if !ok {
		page.Error = "Invalid email or password."
		s.render(w, http.StatusUnauthorized, "login", page)
		return
	}
	if err := s.startSession(w, r, u.ID, t.ID); err != nil {
		s.serverError(w, "start session", err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	if c, err := r.Cookie(s.cookieName()); err == nil && c.Value != "" {
		if err := s.store.DeleteSession(r.Context(), auth.HashToken(c.Value), t.ID); err != nil {
			s.log.Error("delete session", "err", err)
		}
	}
	http.SetCookie(w, s.sessionCookie("", 0))
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *Server) showSetup(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	page := s.page(r, "Set up your shop")

	token := r.URL.Query().Get("token")
	inv, err := s.store.InviteByToken(r.Context(), auth.HashToken(token), t.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		page.Error = invalidLinkMsg
		s.render(w, http.StatusBadRequest, "setup", page)
		return
	case err != nil:
		s.serverError(w, "invite lookup", err)
		return
	}
	page.Token = token
	page.Email = inv.Email
	s.render(w, http.StatusOK, "setup", page)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	page := s.page(r, "Set up your shop")

	if !s.loginRL.allow("setup:"+s.clientIP(r), time.Now()) {
		page.Error = "Too many attempts. Please try again later."
		s.render(w, http.StatusTooManyRequests, "setup", page)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	token := r.PostFormValue("token")
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")
	hash := auth.HashToken(token)

	inv, err := s.store.InviteByToken(r.Context(), hash, t.ID)
	if errors.Is(err, store.ErrNotFound) {
		page.Error = invalidLinkMsg
		s.render(w, http.StatusBadRequest, "setup", page)
		return
	}
	if err != nil {
		s.serverError(w, "invite lookup", err)
		return
	}
	page.Token = token
	page.Email = inv.Email

	if password != confirm {
		page.Error = "Passwords do not match."
		s.render(w, http.StatusBadRequest, "setup", page)
		return
	}
	if n := utf8.RuneCountInString(password); n < 10 || n > 128 {
		page.Error = "Password must be 10 to 128 characters."
		s.render(w, http.StatusBadRequest, "setup", page)
		return
	}

	pwHash, err := auth.HashPassword(password)
	if err != nil {
		s.serverError(w, "hash password", err)
		return
	}
	userID, err := s.store.AcceptInvite(r.Context(), hash, t.ID, pwHash)
	switch {
	case errors.Is(err, store.ErrNotFound):
		page.Token = ""
		page.Error = invalidLinkMsg
		s.render(w, http.StatusBadRequest, "setup", page)
		return
	case errors.Is(err, store.ErrEmailTaken):
		page.Token = ""
		page.Error = "An account with this email already exists. Log in instead."
		s.render(w, http.StatusConflict, "setup", page)
		return
	case err != nil:
		s.serverError(w, "accept invite", err)
		return
	}

	if err := s.startSession(w, r, userID, t.ID); err != nil {
		s.serverError(w, "start session", err)
		return
	}
	s.log.Info("shop owner activated", "shop", t.Subdomain)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) showDashboard(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	notes, err := s.store.ListNotes(r.Context(), t.ID, 50)
	if err != nil {
		s.serverError(w, "list notes", err)
		return
	}
	page := s.page(r, "Admin: "+t.Name)
	page.UserEmail = u.Email
	page.Role = u.Role
	page.Notes = notes
	s.render(w, http.StatusOK, "dashboard", page)
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
		s.serverError(w, "add note", err)
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}