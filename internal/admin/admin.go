// Package admin provides a read-only, authenticated local dashboard.
package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"github.com/axin7/faultberth/internal/password"
	"github.com/axin7/faultberth/internal/store"
)

type Handler struct {
	Store  *store.Store
	Secret []byte
}

type page struct {
	Title       string
	Issues      []store.Issue
	Issue       store.Issue
	Events      []eventView
	Token       string
	Query       string
	Status      string
	Previous    int
	Next        int
	ListOffset  int
	HasPrevious bool
	HasNext     bool
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	user, passwordText, ok := r.BasicAuth()
	hash, err := h.Store.PasswordHash(r.Context(), user)
	if !ok || err != nil || !password.Verify(hash, passwordText) {
		w.Header().Set("WWW-Authenticate", `Basic realm="Faultberth", charset="UTF-8"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/issues/") {
		h.resolve(w, r, user)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch {
	case r.URL.Path == "/":
		h.index(w, r)
	case strings.HasPrefix(r.URL.Path, "/issues/"):
		h.issue(w, r, user)
	default:
		http.NotFound(w, r)
	}
}

func (h Handler) index(w http.ResponseWriter, r *http.Request) {
	offset := pageOffset(r, 50)
	query, status := filterValues(r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	issues, err := h.Store.Issues(r.Context(), offset, query, status)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	render(w, page{Title: "Issues", Issues: issues, Query: query, Status: status, ListOffset: offset,
		Previous: offset - 50, Next: offset + 50, HasPrevious: offset > 0, HasNext: len(issues) == 50})
}

func filterValues(query, status string) (string, string) {
	query = strings.TrimSpace(query)
	if runes := []rune(query); len(runes) > 200 {
		query = string(runes[:200])
	}
	if status != "open" && status != "resolved" {
		status = ""
	}
	return query, status
}

func (h Handler) issue(w http.ResponseWriter, r *http.Request, user string) {
	id := strings.TrimPrefix(r.URL.Path, "/issues/")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		http.NotFound(w, r)
		return
	}
	offset := pageOffset(r, 20)
	query, status := filterValues(r.URL.Query().Get("q"), r.URL.Query().Get("status"))
	listOffset := parseOffset(r.URL.Query().Get("list_offset"), 50)
	issue, events, err := h.Store.IssueEvents(r.Context(), id, offset)
	if store.IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	render(w, page{Title: issue.Title, Issue: issue, Events: eventViews(events), Token: h.token(user, id),
		Query: query, Status: status, ListOffset: listOffset,
		Previous: offset - 20, Next: offset + 20, HasPrevious: offset > 0, HasNext: len(events) == 20})
}

func pageOffset(r *http.Request, pageSize int) int {
	return parseOffset(r.URL.Query().Get("offset"), pageSize)
}

func parseOffset(value string, pageSize int) int {
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 || offset > 1_000_000 || offset%pageSize != 0 {
		return 0
	}
	return offset
}

func (h Handler) token(user, id string) string {
	mac := hmac.New(sha256.New, h.Secret)
	_, _ = mac.Write([]byte(user + "\x00" + id))
	return hex.EncodeToString(mac.Sum(nil))
}

func (h Handler) resolve(w http.ResponseWriter, r *http.Request, user string) {
	id := strings.TrimPrefix(r.URL.Path, "/issues/")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" || len(h.Secret) < 32 {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || !hmac.Equal([]byte(r.PostForm.Get("csrf")), []byte(h.token(user, id))) {
		http.Error(w, "invalid request", http.StatusForbidden)
		return
	}
	action := r.PostForm.Get("action")
	if action != "resolve" && action != "reopen" {
		http.Error(w, "invalid action", http.StatusBadRequest)
		return
	}
	if err := h.Store.SetResolved(r.Context(), id, action == "resolve"); err != nil {
		if store.IsNotFound(err) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "database error", http.StatusInternalServerError)
		}
		return
	}
	query, status := filterValues(r.PostForm.Get("q"), r.PostForm.Get("status"))
	listOffset := parseOffset(r.PostForm.Get("list_offset"), 50)
	http.Redirect(w, r, issueURL(id, query, status, listOffset, 0), http.StatusSeeOther)
}
