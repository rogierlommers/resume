# Resume
My resume, hosted at https://rogier.lommers.org/.

# Local Development

Run the site locally from any working directory:

```bash
./run.sh
```

The server starts on `http://localhost:8080` and serves files from `src/assets`. Set `ADDRESS` to override the listen address.
Set `ACCESS_LOGS=true` to enable request access logs. The default is `false`; only the exact value `true` enables them.

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
