# Resume
My resume, hosted at https://rogier.lommers.org/.

# Local Development

Run the site locally from any working directory:

```bash
./run.sh
```

The server starts on `http://localhost:8080` and serves files from `src/assets`. Set `ADDRESS` to override the listen address.
Set `ACCESS_LOGS=true` to enable request access logs. The default is `false`; only the exact value `true` enables them. Each access log includes the method, path, status, duration, response size, remote IP, host, HTTP protocol, user agent, and referrer. Query strings are intentionally omitted because they may contain sensitive values.

Set `VISITS_TOKEN` to enable the in-memory analytics dashboard at `/visits`. Sign in with any username and use the token as the HTTP Basic Auth password; expose the endpoint only over HTTPS so the credential remains encrypted in transit. If the token is unset, the endpoint returns `503 Service Unavailable`. The dashboard shows totals, unique IPs, daily visits, top paths, referrers and user agents for the current and previous six UTC calendar days, a 30-day daily visits graph, and the latest 1,000 requests. It also estimates the memory retained by those records and aggregates; this estimate excludes the rest of the Go process and container. The dashboard counts every request except `/visits`, `/visits.css`, and `/favicon.ico`, omits query strings, and resets whenever the server restarts. IP addresses identify the direct network peer; forwarded client-address headers are not trusted.

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
