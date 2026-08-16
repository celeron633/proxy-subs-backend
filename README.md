# Proxy Subs Backend

一个轻量的私有订阅文件服务。它提供独立的网页控制台，使用 SQLite 保存配置，并为每条订阅设置独立 token。

## 功能

- 首次打开网页时创建管理员账号，之后必须登录才能管理
- 在网页中新增、编辑、删除订阅，配置 URL 标识和本地文件
- 每条订阅使用独立 token；数据库只保存 token 的 SHA-256 哈希
- 全局服务开关和单条订阅开关均可在网页中操作，并持久化到 SQLite
- 管理员密码使用 bcrypt 保存，登录会话有效期为 7 天
- HTML、CSS、JavaScript 位于独立的 `web/` 目录，不会打包进可执行文件，可单独替换更新
- SQLite 使用纯 Go 驱动，构建时不需要 CGO

## 快速开始

需要 Go 1.25.6 或更高版本。

```bash
go build -o proxy-subs-backend .
./proxy-subs-backend
```

打开 `http://服务器地址:8080/`，首次访问会进入管理员初始化页面。

默认启动参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-listen` | `0.0.0.0:8080` | HTTP 监听地址 |
| `-db` | `data/proxy-subs.db` | SQLite 数据库文件 |
| `-web-dir` | `web` | 网页文件目录 |
| `-debug` | `false` | 是否启用 Gin 调试模式和 HTTP 请求日志 |

例如：

```bash
./proxy-subs-backend -listen 127.0.0.1:9000 -db /var/lib/proxy-subs/app.db -web-dir /opt/proxy-subs/web
```

Linux 下也可以使用仓库中的脚本：

```bash
make release
./start.sh
./stop.sh
```

## 使用订阅地址

在控制台创建 URL 标识为 `clash-main` 的订阅后，可使用：

```text
http://服务器地址:8080/api/clash-main?token=该订阅的token
```

也支持请求头，避免 token 出现在 URL 中：

```bash
curl -H "Authorization: Bearer 该订阅的token" \
  http://服务器地址:8080/api/clash-main
```

URL 标识采用精确匹配，不再使用旧版本的“路径包含 tag”逻辑，避免不同订阅误匹配。

## 数据和升级

- 所有运行数据都在 `-db` 指定的 SQLite 文件中；请定期备份该文件。
- 网页资源在 `web/` 目录中。登录页为 `index.html`，管理页为 `dashboard.html`；`assets/` 下按公共、登录和管理页面拆分样式与脚本。只修改界面时可直接替换这些文件并刷新浏览器，无需重新编译 Go 程序。
- 旧版 JSON 配置和全局 token 文件不再读取。升级前请记下原有订阅配置，并在首次登录后从网页重新添加。旧的全局 token 可以分别填入需要兼容的订阅。
- token 创建或重置后只完整显示一次。遗失后在编辑页面设置新 token 即可。

## 开发检查

```bash
go test ./...
go vet ./...
```

## GitHub Actions 打包

工作流支持手动运行；每次推送到 `master` 都会自动构建最新包，推送 `v*` 标签时还会创建 GitHub Release。只构建以下两个目标：

- `proxy-subs-backend-windows-amd64.zip`
- `proxy-subs-backend-linux-amd64.tar.gz`

两个压缩包都包含程序、`web/`、`static/`、README 和 LICENSE；Linux 包还包含启动和停止脚本。标签构建会同时创建 GitHub Release，手动构建则可从 Actions Artifacts 下载。

项目不会把网页资源嵌入 ELF 或 EXE，因此运行时必须保留 `web/` 目录。
