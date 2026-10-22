package web

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/KING-CYBERTON/ayopa/internal/store"
)

// postBlock is one paragraph or heading of a post body. Bodies are plain
// text: blank lines separate paragraphs and a line starting "## " is a heading.
type postBlock struct {
	Heading bool
	Text    string
}

func postBlocks(body string) []postBlock {
	var out []postBlock
	var para []string
	flush := func() {
		if len(para) > 0 {
			out = append(out, postBlock{Text: strings.Join(para, "\n")})
			para = nil
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, " \t")
		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			out = append(out, postBlock{Heading: true, Text: strings.TrimSpace(line[3:])})
		case strings.TrimSpace(line) == "":
			flush()
		default:
			para = append(para, line)
		}
	}
	flush()
	return out
}

var productLinkRE = regexp.MustCompile(`/shop/(\d{1,18})\b`)

// linkedProductIDs finds product links such as /shop/42 in a post body, so
// the post can show those products underneath. At most eight are kept.
func linkedProductIDs(body string) []int64 {
	var ids []int64
	for _, m := range productLinkRE.FindAllStringSubmatch(body, -1) {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil || id <= 0 || slices.Contains(ids, id) {
			continue
		}
		if ids = append(ids, id); len(ids) == 8 {
			break
		}
	}
	return ids
}

func readMinutes(body string) int {
	return max(1, (len(strings.Fields(body))+199)/200)
}

// ---- storefront ----

func (s *Server) showBlog(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	posts, err := s.store.ListPosts(r.Context(), t.ID, true)
	if err != nil {
		s.serverError(w, "list posts", err)
		return
	}
	pg := s.page(r, "Blog – "+t.Name)
	pg.Posts = posts
	pg.HasBlog = len(posts) > 0
	pg.MetaDesc = "News and stories from " + t.Name + "."
	s.render(w, http.StatusOK, "blog", pg)
}

func (s *Server) showPost(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	post, err := s.store.GetPost(r.Context(), t.ID, id, true)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "get post", err)
		return
	}
	related, err := s.store.PublishedProductsByIDs(r.Context(), t.ID, linkedProductIDs(post.Body))
	if err != nil {
		s.serverError(w, "post products", err)
		return
	}
	pg := s.page(r, post.Title+" – "+t.Name)
	pg.Post = post
	pg.PostBlocks = postBlocks(post.Body)
	pg.Related = related
	pg.Currency = t.Currency
	pg.HasBlog = true
	pg.MetaDesc = post.Excerpt
	s.render(w, http.StatusOK, "post", pg)
}

// ---- admin ----

func (s *Server) showAdminBlog(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	posts, err := s.store.ListPosts(r.Context(), t.ID, false)
	if err != nil {
		s.serverError(w, "list posts", err)
		return
	}
	pg := s.page(r, "Blog — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Posts = posts
	if r.URL.Query().Get("deleted") == "1" {
		pg.Notice = "Post deleted."
	}
	s.render(w, http.StatusOK, "admin_blog", pg)
}

func (s *Server) showPostForm(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	u, ok := s.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	pg := s.page(r, "New post — "+t.Name)
	pg.UserEmail = u.Email
	pg.Role = u.Role
	pg.Post = store.Post{Status: "draft"}
	if idStr := r.PathValue("id"); idStr != "" {
		id, err := parseID(idStr)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		post, err := s.store.GetPost(r.Context(), t.ID, id, false)
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.serverError(w, "get post", err)
			return
		}
		pg.Title = "Edit: " + post.Title + " — " + t.Name
		pg.Post = post
		pg.IsEdit = true
		switch r.URL.Query().Get("saved") {
		case "published":
			pg.Notice = "Post published."
		case "draft":
			pg.Notice = "Saved as draft."
		}
	}
	s.render(w, http.StatusOK, "admin_post_form", pg)
}

// handleSavePost creates a post, or updates one when the path has an id.
// The submit button decides the status: Publish or Save draft.
func (s *Server) handleSavePost(w http.ResponseWriter, r *http.Request) {
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
	post := store.Post{
		Title:    strings.TrimSpace(r.PostFormValue("title")),
		Excerpt:  strings.TrimSpace(r.PostFormValue("excerpt")),
		Body:     strings.TrimSpace(r.PostFormValue("body")),
		Category: strings.TrimSpace(r.PostFormValue("category")),
		Status:   "draft",
	}
	if r.PostFormValue("action") == "publish" {
		post.Status = "published"
	}
	isEdit := r.PathValue("id") != ""
	if isEdit {
		id, err := parseID(r.PathValue("id"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		post.ID = id
	}

	var msg string
	switch {
	case utf8.RuneCountInString(post.Title) < 1 || utf8.RuneCountInString(post.Title) > 120:
		msg = "Title must be 1 to 120 characters."
	case utf8.RuneCountInString(post.Excerpt) > 240:
		msg = "Excerpt must be 240 characters or fewer."
	case utf8.RuneCountInString(post.Body) > 20000:
		msg = "Body must be 20,000 characters or fewer."
	case utf8.RuneCountInString(post.Category) > 40:
		msg = "Category must be 40 characters or fewer."
	}
	if msg != "" {
		pg := s.page(r, "Blog — "+t.Name)
		pg.UserEmail = u.Email
		pg.Role = u.Role
		pg.Error = msg
		pg.Post = post
		pg.IsEdit = isEdit
		s.render(w, http.StatusBadRequest, "admin_post_form", pg)
		return
	}

	var err error
	if isEdit {
		post, err = s.store.UpdatePost(r.Context(), t.ID, post)
	} else {
		post, err = s.store.CreatePost(r.Context(), t.ID, post)
	}
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, "save post", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/admin/blog/%d?saved=%s", post.ID, post.Status), http.StatusSeeOther)
}

func (s *Server) handleDeletePost(w http.ResponseWriter, r *http.Request) {
	t := tenantFrom(r.Context())
	if _, ok := s.currentUser(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeletePost(r.Context(), t.ID, id); errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, "delete post", err)
		return
	}
	http.Redirect(w, r, "/admin/blog?deleted=1", http.StatusSeeOther)
}
