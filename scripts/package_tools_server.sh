#!/usr/bin/env bash
set -euo pipefail

TOOLS_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_FILE="${1:-$TOOLS_ROOT/dist/tools-server.tar.gz}"

# Explicit deployment inputs: usage-example and private values.yaml are excluded.
FILES=(
  codehost-go/go.mod codehost-go/go.sum codehost-go/Dockerfile
  codehost-go/api codehost-go/cmd codehost-go/internal
  deploy/compose.yaml deploy/README.md
  scripts/deploy.sh scripts/package_tools_server.sh
  README.md structure/README.md values.example.yaml .dockerignore .gitignore
)
for relative in "${FILES[@]}"; do
  if [[ ! -e "$TOOLS_ROOT/$relative" ]]; then
    echo "Missing deployment input: $relative" >&2
    exit 1
  fi
done

mkdir -p -- "$(dirname -- "$OUTPUT_FILE")"
STAGING="$(mktemp -d)"
trap 'rm -rf -- "$STAGING"' EXIT
for relative in "${FILES[@]}"; do
  mkdir -p -- "$(dirname -- "$STAGING/tools-server/$relative")"
  cp -a -- "$TOOLS_ROOT/$relative" "$STAGING/tools-server/$relative"
done
chmod 755 "$STAGING/tools-server/scripts/"*.sh
tar -C "$STAGING" -czf "$OUTPUT_FILE" tools-server
echo "$OUTPUT_FILE"
