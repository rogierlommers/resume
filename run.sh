#!/usr/bin/env bash
set -euo pipefail

echo "----------------------"
echo "running local instance"
echo "----------------------"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR/src"
exec go run main.go
