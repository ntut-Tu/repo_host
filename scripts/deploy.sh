#!/usr/bin/env bash
set -euo pipefail

BUNDLE_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd -- "$BUNDLE_ROOT"

if [[ ! -f values.yaml ]]; then
  echo "Missing values.yaml. Copy values.example.yaml and configure codehost_go first." >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "Docker with Compose is required on the server." >&2
  exit 1
fi
if [[ "$(id -u)" -eq 0 ]]; then
  echo "Run deployment as the non-root owner of values.yaml." >&2
  exit 1
fi
if [[ ! -r values.yaml ]]; then
  echo "values.yaml must be readable by the deploying user." >&2
  exit 1
fi

export CODEHOST_UID="$(id -u)"
export CODEHOST_GID="$(id -g)"

docker compose -f deploy/compose.yaml config --quiet
docker compose -f deploy/compose.yaml up -d --build --wait
docker compose -f deploy/compose.yaml ps
