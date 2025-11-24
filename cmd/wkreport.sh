#!/usr/bin/env bash
set -euo pipefail

# Resolve repository root from this script's location.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
BIN_PATH="${REPO_ROOT}/wkreport"

# Build the binary if it is missing.
if [[ ! -x "${BIN_PATH}" ]]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "wkreport binary missing and Go toolchain not found in PATH." >&2
    exit 1
  fi
  echo "wkreport binary not found; building it now..." >&2
  (cd "${REPO_ROOT}" && go build -o "${BIN_PATH}" ./cmd/wkreport)
fi

cd "${REPO_ROOT}"
exec "${BIN_PATH}" "$@"
