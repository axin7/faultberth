// Package ingest receives Sentry SDK error envelopes.
package ingest

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/axin7/faultberth/internal/envelope"
)

const maxBody = 1 << 20

var eventIDPattern = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

type Store interface {
	ProjectKey(ctx context.Context, project string) (string, error)
	SaveEnvelope(ctx context.Context, project string, eventID string, payload []byte,
		env envelope.Envelope) error
}

type Handler struct{ Store Store }

type headers struct {
	EventID string `json:"event_id"`
	DSN     string `json:"dsn"`
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	project, ok := projectID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	env, err := readEnvelope(w, r)
	if err != nil {
		http.Error(w, "invalid envelope", http.StatusBadRequest)
		return
	}
	var meta headers
	if json.Unmarshal(env.Headers, &meta) != nil {
		http.Error(w, "invalid envelope headers", http.StatusBadRequest)
		return
	}
	authenticated, err := authenticate(r, h.Store, project, meta.DSN)
	if err != nil {
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	if !authenticated {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	event, id, err := extractEvent(env, meta.EventID)
	if err != nil {
		http.Error(w, "unsupported or invalid envelope", http.StatusBadRequest)
		return
	}
	if err := h.Store.SaveEnvelope(r.Context(), project, id, event, env); err != nil {
		http.Error(w, "event not saved", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func projectID(path string) (string, bool) {
	parts := strings.Split(path, "/")
	if len(parts) != 5 || parts[1] != "api" || parts[3] != "envelope" || parts[4] != "" {
		return "", false
	}
	if parts[2] == "" || len(parts[2]) > 20 || strings.Trim(parts[2], "0123456789") != "" {
		return "", false
	}
	return parts[2], true
}

func readEnvelope(w http.ResponseWriter, r *http.Request) (envelope.Envelope, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var reader io.Reader = r.Body
	switch r.Header.Get("Content-Encoding") {
	case "", "identity":
	case "gzip":
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			return envelope.Envelope{}, err
		}
		defer gz.Close()
		reader = gz
	default:
		return envelope.Envelope{}, errors.New("unsupported content encoding")
	}
	return envelope.Parse(reader, maxBody)
}

func authenticate(r *http.Request, store Store, project, dsn string) (bool, error) {
	expected, err := store.ProjectKey(r.Context(), project)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if expected == "" {
		return false, nil
	}
	keys, ok := requestKeys(r, dsn, project)
	if !ok || len(keys) == 0 {
		return false, nil
	}
	for _, key := range keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(expected)) != 1 {
			return false, nil
		}
	}
	return true, nil
}

func requestKeys(r *http.Request, dsn, project string) ([]string, bool) {
	var keys []string
	if values, exists := r.URL.Query()["sentry_key"]; exists {
		keys = append(keys, values...)
	}
	if auth := r.Header.Get("X-Sentry-Auth"); auth != "" {
		if !strings.HasPrefix(auth, "Sentry ") {
			return nil, false
		}
		for _, field := range strings.Split(strings.TrimPrefix(auth, "Sentry "), ",") {
			key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
			if ok && key == "sentry_key" {
				keys = append(keys, value)
			}
		}
	}
	if dsn != "" {
		u, err := url.Parse(dsn)
		if err != nil || u.User == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
			strings.TrimSuffix(u.Path, "/") != "/"+project {
			return nil, false
		}
		keys = append(keys, u.User.Username())
	}
	return keys, true
}

func extractEvent(env envelope.Envelope, id string) ([]byte, string, error) {
	var event []byte
	for _, item := range env.Items {
		switch item.Type {
		case "event":
			if event != nil {
				return nil, "", errors.New("multiple events")
			}
			event = item.Payload
		default:
			continue
		}
	}
	if event == nil {
		if id == "" {
			generated := make([]byte, 16)
			if _, err := rand.Read(generated); err != nil {
				return nil, "", err
			}
			id = hex.EncodeToString(generated)
		}
		if !eventIDPattern.MatchString(id) {
			return nil, "", errors.New("invalid envelope id")
		}
		return nil, strings.ToLower(id), nil
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(event, &payload) != nil || payload == nil {
		return nil, "", errors.New("invalid event")
	}
	if id == "" {
		_ = json.Unmarshal(payload["event_id"], &id)
	}
	if !eventIDPattern.MatchString(id) {
		return nil, "", errors.New("invalid event id")
	}
	return event, strings.ToLower(id), nil
}
