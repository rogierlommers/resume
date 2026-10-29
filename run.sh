#!/usr/bin/env bash
set -euo pipefail

echo "----------------------"
echo "running local instance"
echo "----------------------"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT_DIR/src"
export ACCESS_LOGS=false
export VISITS_USERNAME=rogier
export VISITS_TOKEN=justtesting
export RECORD_404_VISITS=true
export VISITS_DB_PATH="$ROOT_DIR/data/visits.db"
exec go run .
