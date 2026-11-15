# Faultberth

Faultberth is an independent, self-hosted error tracker written in Go. It accepts Sentry SDK
error envelopes, stores them in SQLite, and provides a private dashboard for reviewing issues.
It is an early implementation, not a full Sentry or Bugsink replacement.

## Capabilities and limits

- Sentry envelope ingestion with project-key authentication, gzip, bounded request sizes, and
  durable storage before acknowledging a request. Additional item types are stored as part of
  the original envelope; they are not processed or shown as separate attachments.
- Error grouping, a read-only event view, and resolving or reopening issues. A recurring event
  reopens its issue. The dashboard is restricted to administrators.
- Imports projects, users, issues, inline events, and releases from an offline Bugsink SQLite
  snapshot. Existing project IDs, keys, administrator password hashes and raw events are retained.
  Team permissions, tags as searchable indexes, notifications, source maps, and issue timelines
  are not migrated into usable application features. Keep the original snapshot as an archive.
- No automatic retention, rate limiting, HTTPS, backup scheduler, or account recovery. Deploy
  behind a properly configured reverse proxy and arrange backups and disk monitoring.

## Run

Requires Go 1.25.5 or newer. The server binds to a loopback IP only. Keep the dashboard private;
HTTP Basic credentials must only travel over HTTPS or an SSH tunnel.

```sh
go build -o faultberth ./cmd/faultberth
umask 077
printf '%s' 'choose-a-long-unique-password' > admin-password
./faultberth -db faultberth.sqlite -create-admin admin -password-file admin-password
./faultberth -db faultberth.sqlite -create-project 'My application'
./faultberth -db faultberth.sqlite -listen 127.0.0.1:18100
```

Configure your Sentry SDK with `http://<sentry_key>@<host>/<project_id>` using the project ID
and key printed by `-create-project`. A public reverse proxy should expose only the desired
`/api/<project_id>/envelope/` endpoint, use HTTPS, enforce request and rate limits, and leave
all dashboard paths private. The dashboard is at `/`; `/health/ready` checks database access.

## Import from an existing instance

Stop writes or take a consistent SQLite backup using the SQLite backup API. Import into a new,
empty Faultberth database. Do not point Faultberth at the original database as its own database.
Keep the source snapshot, its associated files, and the old deployment intact for rollback.

```sh
./faultberth -db new.sqlite -import-bugsink /path/to/consistent-snapshot.sqlite
```

The importer rejects external or invalid event payloads and imports in one transaction. Verify
project keys, event counts and contents, administrator access, and real SDK ingestion before
switching traffic. Changes written after the snapshot require a new migration or reconciliation;
a rollback also needs reconciliation of any events accepted by Faultberth after switching.

## Development

```sh
go test ./...
go vet ./...
```

The wire format and authentication are based on the public [Sentry envelope specification](https://develop.sentry.dev/sdk/foundations/envelopes/).
