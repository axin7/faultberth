// Package admin provides a read-only, authenticated local dashboard.
package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
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
	Events      []store.Event
	Token       string
	Previous    int
	Next        int
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
	issues, err := h.Store.Issues(r.Context(), offset)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	render(w, page{Title: "Issues", Issues: issues, Previous: offset - 50,
		Next: offset + 50, HasPrevious: offset > 0, HasNext: len(issues) == 50})
}

func (h Handler) issue(w http.ResponseWriter, r *http.Request, user string) {
	id := strings.TrimPrefix(r.URL.Path, "/issues/")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		http.NotFound(w, r)
		return
	}
	offset := pageOffset(r, 20)
	issue, events, err := h.Store.IssueEvents(r.Context(), id, offset)
	if store.IsNotFound(err) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	render(w, page{Title: issue.Title, Issue: issue, Events: events, Token: h.token(user, id),
		Previous: offset - 20, Next: offset + 20, HasPrevious: offset > 0, HasNext: len(events) == 20})
}

func pageOffset(r *http.Request, pageSize int) int {
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
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
	http.Redirect(w, r, "/issues/"+id, http.StatusSeeOther)
}

func render(w http.ResponseWriter, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboard.Execute(w, p)
}

var dashboard = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width">
<title>{{.Title}} | Faultberth</title><style>
body{font:1rem/1.5 system-ui,sans-serif;color:#182136;background:#f5f7fb;margin:0}
header,main{max-width:72rem;margin:auto;padding:1rem} header{border-bottom:1px solid #bcc5d3}
a{color:#0756a5} table{border-collapse:collapse;width:100%;background:white}
th,td{padding:.75rem;text-align:left;border-bottom:1px solid #d5dce5;vertical-align:top}
.table{overflow:auto} pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#fff;padding:1rem}
small{color:#454e5f} details{margin:1rem 0} summary{cursor:pointer}
</style></head><body><header><a href="/">Faultberth</a></header><main>
<h1>{{.Title}}</h1>
{{if .Issue.ID}}<p>Project {{.Issue.ProjectID}} · {{.Issue.Count}} events ·
{{if .Issue.Resolved}}Resolved{{else}}Open{{end}}</p>
<form method="post" action="/issues/{{.Issue.ID}}"><input type="hidden" name="csrf" value="{{.Token}}">
{{if .Issue.Resolved}}<button name="action" value="reopen">Reopen issue</button>
{{else}}<button name="action" value="resolve">Resolve issue</button>{{end}}</form>
{{range .Events}}<details><summary>{{.Timestamp}} · {{.ID}}</summary><pre>{{.Data}}</pre></details>{{end}}
{{else}}<div class="table" role="region" aria-label="Issues" tabindex="0"><table>
<caption>Recent issues</caption><thead><tr><th scope="col">Issue</th><th scope="col">Project</th>
<th scope="col">Events</th><th scope="col">Last seen</th><th scope="col">Status</th></tr></thead><tbody>
{{range .Issues}}<tr><th scope="row"><a href="/issues/{{.ID}}">{{.Title}}</a></th>
<td>{{.ProjectID}}</td><td>{{.Count}}</td><td>{{.LastSeen}}</td>
<td>{{if .Resolved}}Resolved{{else}}Open{{end}}</td></tr>{{else}}<tr><td colspan="5">No issues yet.</td></tr>{{end}}
</tbody></table></div>{{end}}
{{if or .HasPrevious .HasNext}}<nav aria-label="Pagination">
{{if .HasPrevious}}<a href="?offset={{.Previous}}">Previous page</a>{{end}}
{{if .HasNext}}<a href="?offset={{.Next}}">Next page</a>{{end}}</nav>{{end}}
</main></body></html>`))
