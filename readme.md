# resume
My resume, hosted at https://rogier.lommers.org/.

# local development

Run the site locally:

```bash
./run.sh
```

The server starts on `http://localhost:8080` and serves files from `src/assets`.

# build

Build the Linux binary used in the container image:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/resume ./src/*.go
```

Build the Docker image locally:

```bash
docker build -t rogierlommers/resume .
```

# tests

Run the Go tests:

```bash
go test ./src/...
```

# build status

[![Build and push image](https://github.com/rogierlommers/resume/actions/workflows/docker-image.yml/badge.svg)](https://github.com/rogierlommers/resume/actions/workflows/docker-image.yml)
