package store

import (
	"context"
	"strings"
	"testing"

	"github.com/axin7/faultberth/internal/envelope"
)

func TestSaveEventPersistsAndDeduplicates(t *testing.T) {
	s, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("INSERT INTO projects(id,name,sentry_key) VALUES(2,'app','key')"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	payload := []byte(`{"exception":{"values":[{"type":"ValueError","value":"bad"}]}}`)
	for _, id := range []string{"first", "first", "second"} {
		if err := s.SaveEvent(ctx, "2", id, payload); err != nil {
			t.Fatal(err)
		}
	}
	var count, issueCount int
	if err := s.db.QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count FROM issues").Scan(&issueCount); err != nil {
		t.Fatal(err)
	}
	if count != 2 || issueCount != 2 {
		t.Fatalf("events=%d issue count=%d", count, issueCount)
	}
	key, err := s.ProjectKey(ctx, "2")
	if err != nil || key != "key" {
		t.Fatalf("project key %q: %v", key, err)
	}
}

func TestSaveEnvelopePreservesItemsAndDeduplicates(t *testing.T) {
	s, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("INSERT INTO projects(id,name,sentry_key) VALUES(2,'app','key')"); err != nil {
		t.Fatal(err)
	}
	env, err := envelope.Parse(strings.NewReader("{}\n{\"type\":\"attachment\"}\nbytes"), 100)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SaveEnvelope(context.Background(), "2", "id", []byte(`{"message":"x"}`), env); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var data string
	if err := s.db.QueryRow("SELECT count(*),data FROM envelopes").Scan(&count, &data); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !strings.Contains(data, `"attachment"`) || !strings.Contains(data, "Ynl0ZXM=") {
		t.Fatalf("envelope count=%d, data=%q", count, data)
	}
}

func TestSaveEventRejectsUnknownProject(t *testing.T) {
	s, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SaveEvent(context.Background(), "7", "id", []byte(`{"message":"x"}`)); err == nil {
		t.Fatal("accepted nonexistent project")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM issues").Scan(&count); err != nil || count != 0 {
		t.Fatalf("issue count = %d, error %v", count, err)
	}
}

func TestSetup(t *testing.T) {
	s, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateAdmin(ctx, "admin", "too short"); err == nil {
		t.Fatal("accepted short password")
	}
	if err := s.CreateAdmin(ctx, "admin", "long-enough-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PasswordHash(ctx, "admin"); err != nil {
		t.Fatal(err)
	}
	id, key, err := s.CreateProject(ctx, "Test project")
	if err != nil || id != 1 || len(key) != 32 {
		t.Fatalf("project id=%d, key length=%d, err=%v", id, len(key), err)
	}
}

func TestIssuesFilterSearchAndStatus(t *testing.T) {
	s, err := Open(t.TempDir() + "/test.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.Exec("INSERT INTO projects(id,name,sentry_key) VALUES(1,'app','key')"); err != nil {
		t.Fatal(err)
	}
	for _, issue := range []struct {
		id, title string
		resolved  bool
	}{{"a", "Network timeout", false}, {"b", "Network error", true}, {"c", "Disk error", false}} {
		if _, err := s.db.Exec(`INSERT INTO issues(id,project_id,title,first_seen,last_seen,count,resolved)
			VALUES(?,1,?,'a','b',1,?)`, issue.id, issue.title, issue.resolved); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		query, status string
		want          int
	}{{"network", "", 2}, {"network", "open", 1}, {"network", "resolved", 1}, {"%", "", 0}, {"", "open", 2}} {
		issues, err := s.Issues(context.Background(), 0, test.query, test.status)
		if err != nil || len(issues) != test.want {
			t.Fatalf("query=%q status=%q: got %d, want %d, err=%v", test.query, test.status, len(issues), test.want, err)
		}
	}
}
