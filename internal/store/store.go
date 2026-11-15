// Package store persists projects, accounts and events in SQLite.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/axin7/faultberth/internal/envelope"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

type Issue struct {
	ID, Title, LastSeen string
	ProjectID           int64
	Count               int64
	Resolved            bool
}

type Event struct {
	ID, IssueID, Timestamp, Data string
	ProjectID                    int64
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, sentry_key TEXT NOT NULL UNIQUE,
  deleted INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS users (
  username TEXT PRIMARY KEY, password_hash TEXT NOT NULL, active INTEGER NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS issues (
  id TEXT PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id),
  title TEXT NOT NULL, first_seen TEXT NOT NULL, last_seen TEXT NOT NULL,
  count INTEGER NOT NULL, resolved INTEGER NOT NULL DEFAULT 0,
  muted INTEGER NOT NULL DEFAULT 0, deleted INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS releases (
  id TEXT PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id),
  version TEXT NOT NULL, date_released TEXT
);
CREATE TABLE IF NOT EXISTS issue_fingerprints (
  project_id INTEGER NOT NULL REFERENCES projects(id), fingerprint TEXT NOT NULL,
  issue_id TEXT NOT NULL REFERENCES issues(id), PRIMARY KEY(project_id, fingerprint)
);
CREATE TABLE IF NOT EXISTS events (
  id TEXT NOT NULL, project_id INTEGER NOT NULL REFERENCES projects(id),
  issue_id TEXT NOT NULL REFERENCES issues(id), timestamp TEXT NOT NULL, data TEXT NOT NULL,
  PRIMARY KEY(project_id, id)
);
CREATE TABLE IF NOT EXISTS envelopes (
  id INTEGER PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id),
  event_id TEXT NOT NULL, received_at TEXT NOT NULL, data TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_issue ON events(issue_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS issues_last_seen ON issues(last_seen DESC);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{"PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL", schema} {
		if _, err := db.Exec(statement); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize database: %w", err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) ProjectKey(ctx context.Context, project string) (string, error) {
	var key string
	err := s.db.QueryRowContext(ctx, "SELECT sentry_key FROM projects WHERE id=? AND deleted=0", project).Scan(&key)
	return key, err
}

func (s *Store) SaveEvent(ctx context.Context, project, eventID string, payload []byte) error {
	return s.save(ctx, project, eventID, payload, nil)
}

func (s *Store) SaveEnvelope(ctx context.Context, project, eventID string, payload []byte,
	env envelope.Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return s.save(ctx, project, eventID, payload, data)
}

func (s *Store) save(ctx context.Context, project, eventID string, payload, data []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if payload != nil {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM events
			WHERE project_id=? AND id=?)`, project, eventID).Scan(&exists); err != nil {
			return err
		} else if exists {
			return nil
		}
	}
	if data != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO envelopes(project_id,event_id,received_at,data)
			VALUES(?,?,?,?)`, project, eventID, now, string(data)); err != nil {
			return err
		}
	}
	if payload != nil {
		if err := saveEvent(ctx, tx, project, eventID, payload, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func saveEvent(ctx context.Context, tx *sql.Tx, project, eventID string, payload []byte, now string) error {
	title, fingerprint := eventIdentity(payload)
	issueID, err := findOrCreateIssue(ctx, tx, project, title, fingerprint, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events(id,project_id,issue_id,timestamp,data)
		VALUES(?,?,?,?,?)`, eventID, project, issueID, now, string(payload))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE issues SET count=count+1,last_seen=?,resolved=0 WHERE id=?`,
		now, issueID)
	if err != nil {
		return err
	}
	return nil
}

func findOrCreateIssue(ctx context.Context, tx *sql.Tx, project, title, fp, now string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT issue_id FROM issue_fingerprints
		WHERE project_id=? AND fingerprint=?`, project, fp).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	sum := sha256.Sum256([]byte(project + "\x00" + fp))
	id = hex.EncodeToString(sum[:16])
	if _, err = tx.ExecContext(ctx, `INSERT INTO issues(id,project_id,title,first_seen,last_seen,count)
		VALUES(?,?,?,?,?,0)`, id, project, title, now, now); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO issue_fingerprints(project_id,fingerprint,issue_id)
		VALUES(?,?,?)`, project, fp, id)
	return id, err
}

func eventIdentity(payload []byte) (title, fingerprint string) {
	var event struct {
		Message     string   `json:"message"`
		Logger      string   `json:"logger"`
		Fingerprint []string `json:"fingerprint"`
		Exception   struct {
			Values []struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"values"`
		} `json:"exception"`
	}
	_ = json.Unmarshal(payload, &event)
	title = event.Message
	if n := len(event.Exception.Values); n > 0 {
		exception := event.Exception.Values[n-1]
		title = exception.Type + ": " + exception.Value
	}
	if title == "" {
		title = "Untitled event"
	}
	fingerprint = event.Logger + "\x00" + title
	if len(event.Fingerprint) > 0 {
		b, _ := json.Marshal(event.Fingerprint)
		fingerprint = string(b)
	}
	return title, fingerprint
}
