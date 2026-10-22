package web

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/KING-CYBERTON/ayopa/internal/provision"
	"github.com/KING-CYBERTON/ayopa/internal/store"
)

// lookOption is one choice on the Appearance page.
type lookOption struct {
	Key      string
	Name     string
	Desc     string
	Selected bool
	Locked   bool // reserved by the mall and not the shop's current choice
}

type appearanceForm struct {
	Styles   []lookOption
	Palettes []lookOption
	Themes   []lookOption
	Palette  string // the palette the layout previews are drawn in
	Open     int    // layouts the mall leaves open
}

var styleInfo = map[string][2]string{
	"classic":  {"Classic", "Gradient banner, featured strip and a filter sidebar."},
	"boutique": {"Boutique", "Image-led, serif type, square corners and lots of air."},
	"market":   {"Market", "Search first, category chips and a dense product grid."},
}

var themeInfo = map[string][2]string{
	"auto":  {"Automatic", "Light or dark, following each visitor's device."},
	"light": {"Light", "Always light."},
	"dark":  {"Dark", "Deep tinted surfaces, always."},
}

func buildAppearanceForm(m store.Mall, cur store.Tenant, style, palette, theme string) appearanceForm {
	f := appearanceForm{Palette: palette}
	for _, k := range provision.ShopStyles {
		locked := k != cur.Style && slices.Contains(m.LockedStyles, k)
		if !locked {
			f.Open++
		}
		f.Styles = append(f.Styles, lookOption{Key: k, Name: styleInfo[k][0], Desc: styleInfo[k][1],
			Selected: k == style, Locked: locked})
	}
	for _, k := range provision.Palettes {
		f.Palettes = append(f.Palettes, lookOption{Key: k, Name: strings.ToUpper(k[:1]) + k[1:],
			Selected: k == palette, Locked: k != cur.Palette && slices.Contains(m.LockedPalettes, k)})
	}
	for _, k := range provision.Themes {
		f.Themes = append(f.Themes, lookOption{Key: k, Name: themeInfo[k][0], Desc: themeInfo[k][1],
			Selected: k == theme})
	}
	return f
}

func (s *Server) showAppearance(w http.ResponseWriter, r *http.Request) {
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
	pg := s.page(r, "Appearance — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Look = buildAppearanceForm(mallFrom(r.Context()), t, t.Style, t.Palette, t.Theme)
	if r.URL.Query().Get("saved") == "1" {
		pg.Notice = "Your storefront now uses " + styleInfo[t.Style][0] + " · " +
			strings.ToUpper(t.Palette[:1]) + t.Palette[1:] + ". Changes are live."
	}
	s.render(w, http.StatusOK, "admin_appearance", pg)
}

func (s *Server) handleAppearance(w http.ResponseWriter, r *http.Request) {
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
	m := mallFrom(r.Context())
	style, palette, theme := r.PostFormValue("style"), r.PostFormValue("palette"), r.PostFormValue("theme")

	if err := provision.CheckShopAppearance(m, t, style, palette, theme); err != nil {
		pg := s.page(r, "Appearance — "+t.Name)
		pg.UserEmail = u.Email
		pg.Role = u.Role
		if errors.Is(err, provision.ErrLockedByMall) {
			pg.Error = "That option is reserved by " + m.Name + ". Pick another."
		} else {
			pg.Error = "Pick a layout, a palette and a colour mode."
		}
		pg.Look = buildAppearanceForm(m, t, t.Style, t.Palette, t.Theme)
		s.render(w, http.StatusBadRequest, "admin_appearance", pg)
		return
	}
	if err := s.store.SetTenantAppearance(r.Context(), t.ID, style, palette, theme); err != nil {
		s.serverError(w, "update appearance", err)
		return
	}
	http.Redirect(w, r, "/admin/appearance?saved=1", http.StatusSeeOther)
}
