# DaSSLm 管理端

管理端统一使用 **Art Design Pro / Vue 3 / TypeScript / Vite / Element Plus**。
登录、菜单、路由、标签页、主题、布局设置、移动端导航，以及 DNS、SSL、部署、任务、通知、日志等业务页面，
全部是原生 Vue 单文件组件，不再依赖 React、Ant Design 或 Pro Components。

## 开发与构建

需要 Node.js 22 或更高版本。

```bash
npm ci
npm run dev
npm run build
npm run preview
```

开发服务默认监听 `http://localhost:5173`，将 `/api` 代理到 `http://127.0.0.1:8080`。
生产构建写入 `dist/`，使用同源 `/api`，可继续由 Go 服务嵌入和提供。

可在 `.env.local` 中配置：

```dotenv
VITE_API_URL=/api
VITE_API_PROXY_URL=http://127.0.0.1:8080
```

`UMI_APP_API_BASE` 作为旧配置仍兼容；使用它时不要同时设置 `VITE_API_URL`。
登录凭证保存在 `dasslm_token`（旧版 `qdl_token` 会自动迁移）。受保护接口返回 401 时，清除凭证并跳转到登录页，登录后恢复原访问地址。

## 目录

- `src/views/index`：Art Design Pro 官方布局容器。
- `src/components/core`、`src/hooks/core`、`src/store`：框架组件、主题和状态管理。
- `src/router/modules/index.ts`：DaSSLm 的业务菜单与路由，沿用旧 URL。
- `src/views/auth/login`：连接 Go 登录接口的登录页。
- `src/views/dasslm`：各业务页面（仪表盘、域名、解析记录、证书、自动任务、账号、日志、设置、用户、通知渠道）。
- `src/components/dasslm`：业务通用组件，例如服务商图标、卡片单选、代理配置、部署目标编辑器、可复制文本块、详情抽屉。
- `src/components/dasslm/meta.ts`：服务商、状态、文案等常量与工具函数。
- `src/api/dasslm.ts`：业务接口封装，统一错误提示。
- `src/services/client.mjs`：底层 Go API 请求客户端。

## 页面约定

- 列表页使用 `ArtTableHeader` + `ArtTable` + `useTable`（通过 `useResourceTable` 对接服务端的 `page` / `pageSize` 分页）。
- 新建/编辑使用 `ElDrawer` 或 `ElDialog` + `ElForm`，行内操作使用 `TableAction`。
- 抽屉内的子列表使用 `RemoteTable`，自行管理分页与加载状态。
- 图标使用 Remix Icon（`ri:*`），构建前由 `scripts/generate-icons.mjs` 扫描源码并打包到本地，无需访问 Iconify 公共服务。

## 验证

```bash
npm run typecheck
npm test
npx playwright install chromium
npm run build
npm run test:e2e
```

如使用已安装的 Chrome，可通过 `PLAYWRIGHT_EXECUTABLE_PATH` 指定可执行文件。
浏览器测试使用模拟的 Go API，覆盖登录、所有业务路由、DNS 新建表单、刷新、
主题、移动端布局、菜单与标签页、退出后重新登录及登录失效；不执行真实云端操作。

## 上游与授权

框架源码来源：[Daymychen/art-design-pro](https://github.com/Daymychen/art-design-pro)，
基于 v3.0.2 系列，导入提交 `f3aaf58eec1a0e988f162352c33862327a484f95`。
保留了官方框架组件、布局、主题与路由注册机制，移除了无关示例，
并调整了登录接口、业务路由、组件类型及模板品牌信息。
上游 MIT 授权与版权声明保存在 `LICENSE.art-design-pro`。
