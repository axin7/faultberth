package store

import (
	"context"
	"database/sql"
)

// mapImportedFingerprints links unambiguous historical events to their issue.
func mapImportedFingerprints(ctx context.Context, source, target *sql.Tx) error {
	rows, err := source.QueryContext(ctx, `SELECT project_id,issue_id,data FROM events_event
		WHERE storage_backend IS NULL ORDER BY ingested_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type key struct {
		project int64
		value   string
	}
	issueByFingerprint := map[key]string{}
	for rows.Next() {
		var project int64
		var issue, data string
		if err := rows.Scan(&project, &issue, &data); err != nil {
			return err
		}
		_, fp := eventIdentity([]byte(data))
		k := key{project, fp}
		if previous, exists := issueByFingerprint[k]; exists && previous != issue {
			issueByFingerprint[k] = ""
		} else if !exists {
			issueByFingerprint[k] = issue
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for k, issue := range issueByFingerprint {
		if issue == "" {
			continue
		}
		if _, err := target.ExecContext(ctx, `INSERT INTO issue_fingerprints
			(project_id,fingerprint,issue_id) VALUES(?,?,?)`, k.project, k.value, issue); err != nil {
			return err
		}
	}
	return nil
}
