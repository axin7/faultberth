# Faultberth

[English](README.md) | [简体中文](README.zh-CN.md)

Faultberth is an independent, self-hosted error tracker written in Go. It accepts Sentry SDK
error envelopes, stores them in SQLite, and provides an administrator dashboard for reviewing
issues. It is an early implementation of part of the public Sentry protocol, not a complete
replacement for Sentry or Bugsink.

## Features

- Receive Sentry envelopes using a project ID and key, including gzip-compressed requests.
  An envelope is limited to 1 MiB, both on the wire and after decompression, and is stored
  before the server acknowledges it.
- Group error events into issues; search issue titles, filter by status, inspect event details
  and stack traces, and resolve or reopen issues. A recurring event reopens its issue.
- Import projects, users, issues, inline events, and releases from an offline Bugsink SQLite
  snapshot, retaining project IDs and keys, administrator password hashes, and raw events.
- Run as a single Go binary with an embedded dashboard and SQLite database; no Node.js or CDN
  is needed at runtime.

The dashboard currently uses English; this repository also provides Chinese documentation.

## Requirements

- Go 1.25.5 or later and OpenSSL for the quick start.
- A writable, private data directory. Faultberth creates its database with mode `0600` and
  rejects an existing database that is not a private regular file.
- A reverse proxy for remote SDK traffic, and HTTPS or an SSH tunnel for dashboard access.
  The server itself only listens on a loopback IP.

## Quick start

Run these commands in a private working directory. The password file contains a generated
administrator password; store it securely so you can sign in later.

```sh
git clone https://github.com/axin7/faultberth.git
cd faultberth
go build -o faultberth ./cmd/faultberth

umask 077
mkdir -p data
openssl rand -base64 32 > admin-password
./faultberth -db data/faultberth.sqlite -create-admin admin -password-file admin-password
./faultberth -db data/faultberth.sqlite -create-project 'My application'
./faultberth -db data/faultberth.sqlite -listen 127.0.0.1:18100
```

Save the `project_id` and `sentry_key` printed by the project creation command. Visit
`http://127.0.0.1:18100/` on the same machine and sign in with `admin` and the password in
`admin-password`. For remote access, use an SSH tunnel or an HTTPS reverse proxy restricted to
administrators. HTTP Basic credentials must not travel over unencrypted remote connections.

### Connect a Sentry SDK

Set the SDK's DSN to `https://<sentry_key>@<host>/<project_id>`, using the values printed above
and the HTTPS host of your reverse proxy. For local testing, the host can be
`127.0.0.1:18100` and the scheme `http`. For example, with the
[Sentry Go SDK](https://docs.sentry.io/platforms/go/):

```go
err := sentry.Init(sentry.ClientOptions{
    Dsn: "https://<sentry_key>@<host>/<project_id>",
})
if err != nil {
    log.Fatal(err)
}
sentry.CaptureMessage("Faultberth test event")
sentry.Flush(2 * time.Second)
```

The example requires the `github.com/getsentry/sentry-go`, `log`, and `time` imports. Send a
test event, then refresh the dashboard to confirm it appears. See the SDK's documentation for
installation and configuration in your application.

### HTTP endpoints

| Endpoint | Purpose |
| --- | --- |
| `POST /api/<project_id>/envelope/` | Receive envelopes authenticated with the project key. |
| `GET /` | Administrator issue dashboard, protected with HTTP Basic authentication. |
| `GET /health/ready` | Return `200 OK` when the database is reachable, otherwise `503`. |

Expose only the required envelope paths to SDK clients. Keep the dashboard and health endpoint
private; apply HTTPS, request limits and rate limiting at the reverse proxy.

## Docker on Linux

The image uses `scratch` and a prebuilt Linux binary. Build for the Docker host's architecture;
replace `amd64` with `arm64` where appropriate. Initialize the database and administrator using
the quick start first, then stop the local server with Ctrl+C. Run the following from the same
directory on a Linux Docker host:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o faultberth ./cmd/faultberth
tar -cf - Dockerfile faultberth | docker build -t faultberth -
sudo chown -R 14237:14237 data
docker run -d --name faultberth --restart unless-stopped \
  --network host --read-only --cap-drop ALL --security-opt no-new-privileges \
  --mount type=bind,source="$(pwd)/data",target=/data \
  faultberth -db /data/faultberth.sqlite -listen 127.0.0.1:18100
```

Only the Dockerfile and binary are sent to the Docker build, excluding the database and password
file. The `chown` command changes ownership of the quick-start data directory.

The container runs as UID/GID `14237`; its data directory must be writable by that user. Host
networking makes the loopback-only listener reachable by the host's reverse proxy. Adapt this
example to your environment and arrange backups before relying on it for production data.

## Command-line options

| Option | Default | Purpose |
| --- | --- | --- |
| `-db` | `faultberth.sqlite` | SQLite database path. |
| `-listen` | `127.0.0.1:18100` | Loopback IP and port for the server. |
| `-create-admin` | — | Create an administrator and exit; requires `-password-file`. |
| `-password-file` | — | Private regular file containing the administrator password, at most 4096 bytes. |
| `-create-project` | — | Create a project, print its ID and key, and exit. |
| `-import-bugsink` | — | Import a Bugsink SQLite snapshot into an empty database and exit. |

Choose only one of `-create-admin`, `-create-project`, or `-import-bugsink` per invocation.
Password files must not grant group or other users permissions. Run `./faultberth -h` for the
built-in option descriptions.

## Import a Bugsink snapshot

1. Stop writes or use the SQLite backup API to create a consistent snapshot. Keep the snapshot,
   associated files, and old deployment intact for rollback.
2. Import into a **new, empty** Faultberth database. Never use the original database as the
   destination:

   ```sh
   ./faultberth -db data/imported.sqlite -import-bugsink /path/to/consistent-snapshot.sqlite
   ```

3. Check the reported counts, project keys, administrator access, event contents, and real SDK
   ingestion before switching any traffic. Reconcile changes written after the snapshot; rollback
   also requires reconciling events accepted by Faultberth after the switch.

The import runs in one transaction and rejects external or invalid event payloads. It does not
turn team permissions, tags as searchable indexes, notifications, source maps, or issue timelines
into usable Faultberth features. Preserve the original data as an archive.

## Current limitations and operations

Additional envelope item types are retained in the original envelope but are not processed or
shown as separate attachments. Faultberth does not provide automatic retention, rate limiting,
HTTPS, scheduled backups, or account recovery. Configure these externally as needed; monitor disk
space and test SQLite-consistent backups and restoration. Evaluate missing features and protocol
coverage against your own SDKs and workflows before replacing an existing service.

## Development

```sh
go test -race ./...
go vet ./...
```

The generated Tailwind stylesheet is committed and embedded in the binary. After changing the
dashboard template or stylesheet, install [pnpm](https://pnpm.io/) and run:

```sh
pnpm install --frozen-lockfile
pnpm run build:css
```

## References and license

The wire format and authentication are based on the public
[Sentry envelope specification](https://develop.sentry.dev/sdk/foundations/envelopes/).
Faultberth is independent of Sentry and Bugsink. Released under the [MIT License](LICENSE).
