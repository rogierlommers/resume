# Resume
My resume, hosted at https://rogier.lommers.org/.

# Local Development

Run the site locally from any working directory:

```bash
./run.sh
```

The server starts on `http://localhost:8080` and serves files from `src/assets`. Set `ADDRESS` to override the listen address.
Set `ACCESS_LOGS=true` to enable request access logs. The default is `false`; only the exact value `true` enables them. Each access log includes the method, path, status, duration, response size, remote IP, host, HTTP protocol, user agent, and referrer. Query strings are intentionally omitted because they may contain sensitive values.

Set both `VISITS_USERNAME` and `VISITS_TOKEN` to enable the private analytics dashboard at `/visits`. Sign in through the HTML login form with that username and use the token as the password. The form supports standard password-manager autocomplete. Successful logins create an HTTP-only, same-site session that expires after 12 hours or when the server restarts; use the dashboard's **Sign out** button to end it earlier. Expose the endpoint only over HTTPS so the credentials and session remain encrypted in transit. When TLS terminates at a reverse proxy, forward `X-Forwarded-Proto: https` so the session cookie receives the `Secure` attribute. If either credential is unset, the endpoint returns `503 Service Unavailable`. The dashboard shows totals, unique IPs, daily visits, top paths, referrers and user agents for the current and previous six UTC calendar days, a 30-day daily visits graph, and the latest 1,000 requests. The dashboard excludes its login, logout and stylesheet requests, `/favicon.ico`, `/healthz`, and responses with an HTTP 404 status from its request counts. Set `RECORD_404_VISITS=true` to include 404 responses. Query strings are omitted. IP addresses identify the direct network peer; forwarded client-address headers are not trusted.

Visits use bounded in-memory storage by default and reset whenever the server restarts. Set `VISITS_DB_PATH` to a SQLite database file (for example, `/data/visits.db`) to persist visits across restarts. The parent directory must already exist and be writable by the server user; mount it as a persistent volume when running the container. SQLite retains the latest 30 UTC calendar days and automatically creates its schema and indexes. Unset `VISITS_DB_PATH` to switch back to in-memory storage without changing or deleting the database file.

# Build

Build the Linux binary used in the container image:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ./bin/resume ./src/*.go
```

Build and push both a commit-tagged and `latest` Docker image. Docker Hub credentials are required:

```bash
./build.sh
```

To build without pushing, run the binary build command above followed by `docker build -t rogierlommers/resume:local .`. The Docker build requires `bin/resume` to exist.

# Tests

Run the Go tests:

```bash
go test -race ./...
```

The health endpoint is available at `/healthz` and returns `ok` when the server is running.

# build status

[![Build and push image](https://github.com/rogierlommers/resume/actions/workflows/docker-image.yml/badge.svg)](https://github.com/rogierlommers/resume/actions/workflows/docker-image.yml)
