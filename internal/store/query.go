package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func (s *Store) PasswordHash(ctx context.Context, username string) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users
		WHERE username=? AND active=1 AND is_admin=1`,
		username).Scan(&hash)
	return hash, err
}

func (s *Store) Issues(ctx context.Context, offset int, query, status string) ([]Issue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,title,last_seen,count,resolved
		FROM issues WHERE deleted=0 AND instr(lower(title),lower(?)) > 0
		AND (?='' OR resolved=?) ORDER BY last_seen DESC,id LIMIT 50 OFFSET ?`,
		query, status, status == "resolved", offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var issues []Issue
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(&issue.ID, &issue.ProjectID, &issue.Title, &issue.LastSeen,
			&issue.Count, &issue.Resolved); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, rows.Err()
}

func (s *Store) IssueEvents(ctx context.Context, id string, offset int) (Issue, []Event, error) {
	var issue Issue
	err := s.db.QueryRowContext(ctx, `SELECT id,project_id,title,last_seen,count,resolved
		FROM issues WHERE id=? AND deleted=0`, id).Scan(&issue.ID, &issue.ProjectID, &issue.Title,
		&issue.LastSeen, &issue.Count, &issue.Resolved)
	if err != nil {
		return Issue{}, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,issue_id,timestamp,data FROM events
		WHERE issue_id=? ORDER BY timestamp DESC,id LIMIT 20 OFFSET ?`, id, offset)
	if err != nil {
		return Issue{}, nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.ProjectID, &event.IssueID, &event.Timestamp,
			&event.Data); err != nil {
			return Issue{}, nil, err
		}
		var pretty any
		if json.Unmarshal([]byte(event.Data), &pretty) == nil {
			if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
				event.Data = string(b)
			}
		}
		events = append(events, event)
	}
	return issue, events, rows.Err()
}

func IsNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (s *Store) SetResolved(ctx context.Context, id string, resolved bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE issues SET resolved=? WHERE id=? AND deleted=0`,
		resolved, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Ready(ctx context.Context) error { return s.db.PingContext(ctx) }
