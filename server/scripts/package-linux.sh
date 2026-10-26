#!/usr/bin/env bash
set -euo pipefail

SERVER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT_DIR="$(cd "${SERVER_DIR}/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
PACKAGE_DIR="${DIST_DIR}/dasslm-server-linux-amd64"

cd "${SERVER_DIR}"
"${SERVER_DIR}/scripts/sync-admin-web.sh"

rm -rf "${PACKAGE_DIR}"
mkdir -p "${PACKAGE_DIR}/config"

GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "${PACKAGE_DIR}/dasslm-server" ./cmd/dasslm-server
cp "${SERVER_DIR}/config/config.example.yaml" "${PACKAGE_DIR}/config/config.yaml"
cp "${SERVER_DIR}/README.md" "${PACKAGE_DIR}/README.md"

tar -C "${DIST_DIR}" -czf "${PACKAGE_DIR}.tar.gz" "$(basename "${PACKAGE_DIR}")"

echo "${PACKAGE_DIR}"
echo "${PACKAGE_DIR}.tar.gz"
