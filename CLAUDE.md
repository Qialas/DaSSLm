# DaSSLm 项目约定

## 打包发布
- 只打包 Linux amd64，不打包 arm64（本地脚本和 GitHub Release 工作流都只出 amd64）。
- 打包命令：`cd server && ./scripts/package-linux.sh`（需要写入版本号时加 `VERSION=x.y.z`）。
- 根目录 `dist/` 只保留最新一版：程序目录 `dist/dasslm-server-linux-amd64/`（含 `dasslm-server` 二进制）和对应的 `.tar.gz` 发布包。
  旧版本的发布包、其他架构的产物和旧的发布说明都要清理；`package-linux.sh` 打包成功后会自动清理。
- 管理端改动后需重新构建并同步到 `server/internal/web/dist`（`server/scripts/sync-admin-web.sh`，`package-linux.sh` 默认会执行）。
