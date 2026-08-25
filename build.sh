#!/usr/bin/env bash
set -euo pipefail

echo "building container"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR"

IMAGE="${IMAGE:-rogierlommers/resume}"
TAG="${TAG:-$(git rev-parse --short=12 HEAD)}"

# build binary
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ./bin/resume ./src/*.go

# build container and push immutable and convenience tags
docker build -t "$IMAGE:$TAG" -t "$IMAGE:latest" .
docker push "$IMAGE:$TAG"
docker push "$IMAGE:latest"
