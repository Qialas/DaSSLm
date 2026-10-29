#!/usr/bin/env bash
# 打包 Linux 发布包（项目约定：只发布 amd64；dist 只保留最新一版的程序目录和发布包）
#   GOARCH=amd64         目标架构，默认 amd64（不再发布 arm64）
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

# 打包成功后清理旧版本与其他架构的产物，dist 只保留本次的程序目录和发布包
find "${DIST_DIR}" -mindepth 1 -maxdepth 1 \
  \( -name 'dasslm-server-linux-*' -o -name 'qdl-server-linux-*' -o -name 'release-notes-*.md' -o -name 'SHA256SUMS.txt' \) \
  ! -path "${PACKAGE_DIR}" ! -path "${ARCHIVE}" -exec rm -rf {} +

echo "${PACKAGE_DIR}"
echo "${ARCHIVE}"
