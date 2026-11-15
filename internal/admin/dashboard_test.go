package admin

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/pbkdf2"

	"github.com/axin7/faultberth/internal/store"
)

func TestDashboardAuthAndEscape(t *testing.T) {
	path := t.TempDir() + "/test.sqlite"
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	digest := pbkdf2.Key([]byte("pass"), []byte("salt"), 2, 32, sha256.New)
	hash := strings.Join([]string{"pbkdf2_sha256", "2", "salt", base64.StdEncoding.EncodeToString(digest)}, "$")
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO users(username,password_hash,active,is_admin) VALUES(?,?,1,1)", []any{"admin", hash}},
		{"INSERT INTO users(username,password_hash,active,is_admin) VALUES(?,?,1,0)", []any{"member", hash}},
		{"INSERT INTO projects(id,name,sentry_key) VALUES(1,'test','key')", nil},
		{`INSERT INTO issues(id,project_id,title,first_seen,last_seen,count)
		VALUES('0123456789abcdef0123456789abcdef',1,'<script>alert(1)</script>','a','b',1)`, nil},
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		username, password string
		status             int
	}{{"", "", 401}, {"member", "pass", 401}, {"admin", "wrong", 401}, {"admin", "pass", 200}} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.SetBasicAuth(test.username, test.password)
		w := httptest.NewRecorder()
		Handler{Store: s}.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("user %q got status %d", test.username, w.Code)
		}
		if w.Code == 200 && (strings.Contains(w.Body.String(), "<script>") ||
			!strings.Contains(w.Body.String(), "&lt;script&gt;")) {
			t.Fatal("issue title was not escaped")
		}
	}
	h := Handler{Store: s, Secret: []byte("0123456789abcdef0123456789abcdef")}
	id := "0123456789abcdef0123456789abcdef"
	r := httptest.NewRequest(http.MethodGet, "/?q=script&status=open", nil)
	r.SetBasicAuth("admin", "pass")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "/issues/"+id+"?q=script&amp;status=open") {
		t.Fatal("issue link did not preserve search and status filters")
	}
	r = httptest.NewRequest(http.MethodGet, "/issues/"+id+"?q=script&status=open", nil)
	r.SetBasicAuth("admin", "pass")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), `href="/?q=script&amp;status=open"`) {
		t.Fatal("detail link did not preserve search and status filters")
	}
	for _, test := range []struct {
		token  string
		status int
	}{{"invalid", 403}, {h.token("admin", id), 303}} {
		form := url.Values{"csrf": {test.token}, "action": {"resolve"}, "q": {"script"}, "status": {"open"}}
		r := httptest.NewRequest(http.MethodPost, "/issues/"+id, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetBasicAuth("admin", "pass")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("resolve status = %d, want %d", w.Code, test.status)
		}
		if w.Code == 303 && w.Header().Get("Location") != "/issues/"+id+"?q=script&status=open" {
			t.Fatalf("resolve redirect lost context: %s", w.Header().Get("Location"))
		}
	}
	var resolved bool
	if err := db.QueryRow("SELECT resolved FROM issues WHERE id=?", id).Scan(&resolved); err != nil || !resolved {
		t.Fatalf("issue not resolved: %v", err)
	}
}
