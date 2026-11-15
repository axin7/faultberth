package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
)

type ImportCounts struct {
	Projects, Users, Issues, Events, Releases int64
}

// ImportBugsink imports an offline SQLite snapshot into an empty database.
func (s *Store) ImportBugsink(ctx context.Context, path string) (ImportCounts, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ImportCounts{}, err
	}
	u := url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}
	source, err := sql.Open("sqlite", u.String())
	if err != nil {
		return ImportCounts{}, err
	}
	defer source.Close()
	if _, err := source.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return ImportCounts{}, err
	}
	readTx, err := source.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ImportCounts{}, err
	}
	defer readTx.Rollback()
	var unsupported int64
	if err := readTx.QueryRowContext(ctx, `SELECT count(*) FROM events_event
		WHERE storage_backend IS NOT NULL OR NOT json_valid(data)`).Scan(&unsupported); err != nil {
		return ImportCounts{}, err
	}
	if unsupported != 0 {
		return ImportCounts{}, errors.New("source has externally stored or invalid events")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportCounts{}, err
	}
	defer tx.Rollback()
	var populated bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects UNION ALL
		SELECT 1 FROM users UNION ALL SELECT 1 FROM issues UNION ALL SELECT 1 FROM events
		UNION ALL SELECT 1 FROM releases UNION ALL SELECT 1 FROM envelopes)`).Scan(&populated); err != nil {
		return ImportCounts{}, err
	}
	if populated {
		return ImportCounts{}, errors.New("destination database is not empty")
	}
	counts := ImportCounts{}
	for _, step := range importSteps(&counts) {
		if err := copyRows(ctx, readTx, tx, step); err != nil {
			return ImportCounts{}, fmt.Errorf("import %s: %w", step.name, err)
		}
	}
	if err := mapImportedFingerprints(ctx, readTx, tx); err != nil {
		return ImportCounts{}, fmt.Errorf("map imported issues: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ImportCounts{}, err
	}
	return counts, nil
}

type importStep struct {
	name, selectSQL, insertSQL string
	columns                    int
	count                      *int64
}

func importSteps(c *ImportCounts) []importStep {
	return []importStep{
		{"projects", `SELECT id,name,sentry_key,is_deleted FROM projects_project`,
			`INSERT INTO projects(id,name,sentry_key,deleted) VALUES(?,?,?,?)`, 4, &c.Projects},
		{"users", `SELECT username,password,is_active,is_superuser FROM auth_user`,
			`INSERT INTO users(username,password_hash,active,is_admin) VALUES(?,?,?,?)`, 4, &c.Users},
		{"issues", `SELECT id,project_id,coalesce(calculated_type,'') || ': ' ||
			coalesce(calculated_value,''),first_seen,last_seen,digested_event_count,
			is_resolved,is_muted,is_deleted FROM issues_issue`,
			`INSERT INTO issues(id,project_id,title,first_seen,last_seen,count,resolved,muted,deleted)
			VALUES(?,?,?,?,?,?,?,?,?)`, 9, &c.Issues},
		{"events", `SELECT event_id,project_id,issue_id,coalesce(timestamp,ingested_at),data
			FROM events_event WHERE storage_backend IS NULL`,
			`INSERT INTO events(id,project_id,issue_id,timestamp,data) VALUES(?,?,?,?,?)`, 5, &c.Events},
		{"releases", `SELECT id,project_id,version,date_released FROM releases_release`,
			`INSERT INTO releases(id,project_id,version,date_released) VALUES(?,?,?,?)`, 4, &c.Releases},
	}
}

func copyRows(ctx context.Context, source, target *sql.Tx, step importStep) error {
	rows, err := source.QueryContext(ctx, step.selectSQL)
	if err != nil {
		return err
	}
	defer rows.Close()
	values, ptrs := make([]any, step.columns), make([]any, step.columns)
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if _, err := target.ExecContext(ctx, step.insertSQL, values...); err != nil {
			return err
		}
		*step.count++
	}
	return rows.Err()
}
