# DaSSLm

> 开源的一站式 DNS 与 SSL 证书管理系统。
> Dns and SSL Manager.

![DaSSLm Preview](https://assets.oss.qialas.com/images/udd1f25b948024046/460ec753bcd340eda675ee81accd4b0d.png)

DaSSLm 面向需要统一管理域名解析、SSL 证书申请、证书资产和自动化部署的个人开发者与小团队。项目采用 Go + MySQL，管理端后台框架使用 Art Design Pro。

## 功能特性

- 多厂商 DNS 账号管理：支持阿里云 DNS、DNSPod / 腾讯云 DNS、Cloudflare 等服务接入。
- 域名资产同步：将多个服务商接入的域名统一编辑/管理解析记录。
- DNS 解析记录管理：支持解析记录列表、线路查询、新增、编辑、删除与同步。
- SSL 账号管理：支持 Let's Encrypt、ZeroSSL、自定义 ACME、腾讯云免费证书、阿里云免费证书等账号类型配置。
- 证书资产管理：独立证书页面，支持证书申请记录、证书状态、到期时间、续签提前天数等管理。
- 腾讯云 SSL 对接：支持腾讯云免费证书申请、DNS 验证记录补全、证书资源拉取与本地保存。
- 自动任务：自动续签证书、自动部署、自动添加对应DNS解析、等。

## 技术栈

| 模块 | 技术 |
| --- | --- |
| 服务端 | Go、Gin、GORM、MySQL、JWT |
| 管理端 | Art Design Pro、Vue 3、TypeScript、Vite、Element Plus |
| DNS 接入 | 阿里云DNS、腾讯云DNS、DNSPod、Cloudflare|
| SSL 接入 | Let's Encrypt、ZeroSSL、LiteSSL、腾讯云免费证书、阿里云免费证书 |
| 自动部署| 宝塔面板、腾讯云内容网络分发、腾讯云COS|

## 项目结构

```text
.
├── admin-web/          # Art Design Pro 管理后台（Vue 3 + Element Plus）
└── server/             # Go 服务端、API、数据库模型与打包脚本
```

## 部署使用

### 宝塔面板

#### 1.创建数据库

安装好基础环境后创建MySQL数据库，并创建一个程序目录，例如`/www/wwwroot/DaSSLm`。

#### 2.下载release包

Linux服务器可以直接在 [release](https://github.com/Qialas/DaSSLm/releases) 中下载打包好的dasslm-server-linux-amd64.tar.gz，上传到对应目录中并解压

#### 3.创建Go项目

| 配置项 | 配置说明 |
| --- | --- |
| 项目执行文件 | /解压目录/dasslm-server |
| 项目名称 | 默认即可（可自定义） |
| 项目端口| 8080（默认，可在config/config.yaml中修改）|

启动用户选择www，其他配置项非必须，启动后程序会自动安装，默认用户名admin 密码123456

## 快速开始


### 1. 准备数据库

先创建 MySQL 数据库，默认库名为 `dasslm`。服务端启动时会自动执行表结构迁移，数据库本身需要提前存在。

```sql
CREATE DATABASE dasslm DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 2. 配置服务端

```bash
cd server
cp config/config.example.yaml config/config.yaml
```

编辑 `server/config/config.yaml`，填写 MySQL 连接信息。`jwt.secret` 保持留空即可，首次启动会自动生成随机密钥并写回配置文件，之后重启继续复用；如需自定义，填写 32 位以上的随机字符串。

### 3. 启动服务端

```bash
cd server
go mod tidy
go run ./cmd/dasslm-server
```

默认监听地址：

```text
http://localhost:8080
```

首次启动时，如果 `server/config/install.lock` 不存在，系统会创建默认管理员并写入安装锁。

```text
用户名：admin
密码：123456
```

生产环境请在首次登录后立即修改默认密码。

### 4. 启动管理端开发环境

```bash
cd admin-web
npm install
npm run dev
```

管理端默认在 `http://localhost:5173` 启动，将 `/api` 代理到 Go 服务 `http://127.0.0.1:8080`。API 地址可通过 `VITE_API_URL` 配置，开发代理目标通过 `VITE_API_PROXY_URL` 配置；生产构建默认使用相对路径 `/api`。详细迁移说明见 [管理端 README](admin-web/README.md)。

## Linux 打包部署

项目提供一键 Linux amd64 打包脚本。脚本会先构建 `admin-web`，再同步到 `server/internal/web/dist` 并嵌入 Go 二进制。

```bash
cd server
chmod +x scripts/package-linux.sh
./scripts/package-linux.sh
```

打包产物：

```text
dist/dasslm-server-linux-amd64/dasslm-server
dist/dasslm-server-linux-amd64.tar.gz
```

上传到服务器后解压，修改 `config/config.yaml`，然后运行：

```bash
./dasslm-server
```

运行后可直接访问服务端端口打开管理后台：

```text
http://服务器IP:8080/
```

## 常用命令

```bash
# 服务端编译检查
cd server && go build ./cmd/dasslm-server

# 服务端测试
cd server && go test ./...

# 管理端构建
cd admin-web && npm run build

# Linux 发布包
cd server && ./scripts/package-linux.sh
```

## 配置说明

服务端配置文件位于 `server/config/config.yaml`：

```yaml
server:
  host: 0.0.0.0
  port: 8080
  mode: debug

database:
  host: 127.0.0.1
  port: 3306
  username: root
  password: ""
  database: dasslm
  charset: utf8mb4
  parseTime: true
  loc: Local

jwt:
  secret: "" # 留空时首次启动自动生成随机密钥并写回本文件；填写则直接使用
  expireHours: 24
```

## 更新说明

### 未发布

- 安全：`jwt.secret` 默认留空，首次启动自动生成随机密钥并写回配置文件；仍使用旧版公开默认值 `please-change-this-secret` 的部署会自动替换（所有用户需重新登录一次）。
- 安全：登录状态以数据库为准，用户被删除、停用，或修改密码、变更角色后，已签发的登录凭证立即失效；登录凭证仅接受 HS256 签名。
- 依赖：升级 `golang-jwt/jwt` 至 v5.3.1、`golang.org/x/net`、`golang.org/x/crypto` 等至最新版本，Go 工具链固定为 1.26.10，`govulncheck` 无可达漏洞；升级前端 Vite 修复开发服务器安全问题。

### v0.0.1

- 管理端业务页面全部改为 Art Design Pro（Vue 3 + Element Plus）原生实现，移除 React、Ant Design 与 Pro Components 依赖。
- 宝塔面板部署账号自动忽略面板 HTTPS 自签名证书。
- 项目更名为 DaSSLm，旧版 `qdl_*` 数据表启动时自动重命名为 `dasslm_*`。

### v1.3.0（旧版 DnsaSSL_Mgr）

- 更新自动任务，支持按证书选择多个部署目标并自动部署。
- 优化代码结构，拆分 DNS、SSL、部署账号和 Handler 模块，便于后续维护。
