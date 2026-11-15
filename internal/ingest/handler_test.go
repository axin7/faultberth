package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/axin7/faultberth/internal/envelope"
)

const id = "0123456789abcdef0123456789abcdef"

type fakeStore struct {
	key, id string
	saved   []byte
	env     envelope.Envelope
	fail    bool
}

func (s *fakeStore) ProjectKey(_ context.Context, project string) (string, error) {
	if project != "2" {
		return "", sql.ErrNoRows
	}
	return s.key, nil
}
func (s *fakeStore) SaveEnvelope(_ context.Context, _, id string, payload []byte,
	env envelope.Envelope) error {
	if s.fail {
		return errors.New("storage unavailable")
	}
	s.id, s.saved, s.env = id, payload, env
	return nil
}

func TestReceive(t *testing.T) {
	body := `{"event_id":"` + id + `"}` + "\n" + `{"type":"event"}` + "\n" +
		`{"message":"test"}` + "\n"
	tests := []struct {
		name, path, key, auth, dsn string
		status                     int
		fail                       bool
	}{
		{"query key", "/api/2/envelope/?sentry_key=key", "key", "", "", 200, false},
		{"auth header", "/api/2/envelope/", "key", "Sentry sentry_key=key", "", 200, false},
		{"no key", "/api/2/envelope/", "key", "", "", 403, false},
		{"wrong key", "/api/2/envelope/?sentry_key=wrong", "key", "", "", 403, false},
		{"conflicting keys", "/api/2/envelope/?sentry_key=key", "key", "Sentry sentry_key=wrong", "", 403, false},
		{"unknown project", "/api/3/envelope/?sentry_key=key", "key", "", "", 403, false},
		{"bad path", "/api/x/envelope/", "key", "", "", 404, false},
		{"database failure", "/api/2/envelope/?sentry_key=key", "key", "", "", 503, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeStore{key: tt.key, fail: tt.fail}
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(body))
			req.Header.Set("X-Sentry-Auth", tt.auth)
			w := httptest.NewRecorder()
			Handler{store}.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
			}
			if tt.status == 200 && (store.id != id || string(store.saved) != `{"message":"test"}`) {
				t.Fatal("event not saved")
			}
		})
	}
}

func TestGzipAndOtherEnvelopeItems(t *testing.T) {
	store := &fakeStore{key: "key"}
	body := `{"event_id":"` + id + `","dsn":"https://key@example.org/2"}` + "\n" +
		`{"type":"event"}` + "\n" + `{"message":"test"}`
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(body))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/2/envelope/", &compressed)
	req.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	Handler{store}.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("gzip status = %d: %s", w.Code, w.Body)
	}
	unsupported := strings.Replace(body, `"event"`, `"attachment"`, 1)
	req = httptest.NewRequest(http.MethodPost, "/api/2/envelope/", strings.NewReader(unsupported))
	w = httptest.NewRecorder()
	Handler{store}.ServeHTTP(w, req)
	if w.Code != 200 || len(store.env.Items) != 1 || store.env.Items[0].Type != "attachment" {
		t.Fatalf("attachment not retained, status = %d", w.Code)
	}
}
