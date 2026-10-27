#!/usr/bin/env bash
# 打包 Linux 发布包
#   GOARCH=amd64|arm64   目标架构，默认 amd64
#   VERSION=0.0.3        写入程序版本号并加到包名中，默认使用代码中的版本、包名不带版本
#   SKIP_WEB_BUILD=1     跳过管理端构建（已同步过 internal/web/dist 时使用）
set -euo pipefail

SERVER_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT_DIR="$(cd "${SERVER_DIR}/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
GOARCH="${GOARCH:-amd64}"
VERSION="${VERSION:-}"
PACKAGE_NAME="dasslm-server-linux-${GOARCH}"
PACKAGE_DIR="${DIST_DIR}/${PACKAGE_NAME}"
ARCHIVE="${DIST_DIR}/${PACKAGE_NAME}${VERSION:+-v${VERSION}}.tar.gz"

cd "${SERVER_DIR}"
if [ "${SKIP_WEB_BUILD:-}" != "1" ]; then
  "${SERVER_DIR}/scripts/sync-admin-web.sh"
fi

rm -rf "${PACKAGE_DIR}"
mkdir -p "${PACKAGE_DIR}/config"

LDFLAGS="-s -w"
if [ -n "${VERSION}" ]; then
  LDFLAGS="${LDFLAGS} -X main.version=${VERSION}"
fi
GOOS=linux GOARCH="${GOARCH}" CGO_ENABLED=0 go build -trimpath -ldflags "${LDFLAGS}" -o "${PACKAGE_DIR}/dasslm-server" ./cmd/dasslm-server
chmod 755 "${PACKAGE_DIR}/dasslm-server"
cp "${SERVER_DIR}/config/config.example.yaml" "${PACKAGE_DIR}/config/config.yaml"
cp "${SERVER_DIR}/README.md" "${PACKAGE_DIR}/README.md"

tar -C "${DIST_DIR}" --owner=0 --group=0 -cf - "${PACKAGE_NAME}" | gzip -9 > "${ARCHIVE}"

echo "${PACKAGE_DIR}"
echo "${ARCHIVE}"
