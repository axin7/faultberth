package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestImportBugsinkAtomicAndReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.sqlite")
	source, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	fixture := []string{
		`CREATE TABLE projects_project(id INTEGER,name TEXT,sentry_key TEXT,is_deleted INTEGER)`,
		`CREATE TABLE auth_user(username TEXT,password TEXT,is_active INTEGER,is_superuser INTEGER)`,
		`CREATE TABLE issues_issue(id TEXT,project_id INTEGER,calculated_type TEXT,calculated_value TEXT,
		first_seen TEXT,last_seen TEXT,digested_event_count INTEGER,is_resolved INTEGER,
		is_muted INTEGER,is_deleted INTEGER)`,
		`CREATE TABLE events_event(event_id TEXT,project_id INTEGER,issue_id TEXT,timestamp TEXT,
		ingested_at TEXT,data TEXT,storage_backend TEXT)`,
		`CREATE TABLE releases_release(id TEXT,project_id INTEGER,version TEXT,date_released TEXT)`,
		`INSERT INTO projects_project VALUES(2,'app','key',0)`,
		`INSERT INTO auth_user VALUES('admin','hash',1,1)`,
		`INSERT INTO issues_issue VALUES('issue',2,'Error','bad','2026-01-01','2026-01-02',1,0,0,0)`,
		`INSERT INTO events_event VALUES('event',2,'issue','2026-01-02','2026-01-02','{}',NULL)`,
		`INSERT INTO releases_release VALUES('release',2,'1.0','2026-01-01')`,
	}
	for _, statement := range fixture {
		if _, err := source.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	defer source.Close()
	destination, err := Open(filepath.Join(t.TempDir(), "destination.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	counts, err := destination.ImportBugsink(context.Background(), path)
	if err != nil || counts != (ImportCounts{1, 1, 1, 1, 1}) {
		t.Fatalf("counts=%+v, err=%v", counts, err)
	}
	if err := destination.SaveEvent(context.Background(), "2", "new-event", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	var issue string
	if err := destination.db.QueryRow(`SELECT issue_id FROM events WHERE id='new-event'`).Scan(&issue); err != nil || issue != "issue" {
		t.Fatalf("new event did not retain its historical issue: %q, %v", issue, err)
	}
	if _, err = destination.ImportBugsink(context.Background(), path); err == nil {
		t.Fatal("import into non-empty database succeeded")
	}
	var count int
	if err = source.QueryRow("SELECT count(*) FROM events_event").Scan(&count); err != nil || count != 1 {
		t.Fatalf("source event count=%d, err=%v", count, err)
	}
}
