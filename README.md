# Proxy Subs Backend

一个轻量的私有订阅文件服务。它提供独立的网页控制台，使用 SQLite 保存配置，并为每条订阅设置独立 token。

## 功能

- 首次打开网页时创建管理员账号，之后必须登录才能管理
- 登录使用 7 位数字与大小写字母验证码，验证码一次性使用并在 5 分钟后过期
- 登录与订阅 API 分别提供按 IP 的连续错误保护，安全开关可在独立设置页管理
- 在网页中新增、编辑、删除订阅，可从限定的服务器目录中选择本地文件
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

打开 `http://服务器地址:8080/`，首次访问会进入管理员初始化页面。初始化账号时不要求验证码，之后登录时需要输入验证码。

默认启动参数：

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-listen` | `127.0.0.1:8080` | HTTP 监听地址 |
| `-db` | `data/proxy-subs.db` | SQLite 数据库文件 |
| `-web-dir` | `web` | 网页文件目录 |
| `-file-root` | `.` | 服务器文件选择器允许浏览的根目录 |
| `-debug` | `false` | 是否启用 Gin 调试模式和 HTTP 请求日志 |

例如：

```bash
./proxy-subs-backend -listen 127.0.0.1:9000 -db /var/lib/proxy-subs/app.db -web-dir /opt/proxy-subs/web -file-root /etc/subscriptions
```

`-file-root` 只限定网页文件选择器的浏览范围。已有订阅和手动输入的文件路径仍可位于其他目录，升级后不会失效。文件选择接口需要管理员登录，并会忽略指向根目录以外的符号链接。

Linux 下也可以使用仓库中的脚本：

```bash
make release
./start.sh
./stop.sh
```

`start.sh` 默认将程序目录作为文件选择根目录，也可在启动时指定：

```bash
FILE_ROOT=/etc/subscriptions ./start.sh
```

Windows 下可在 PowerShell 中运行：

```powershell
.\build.ps1
```

脚本会以 `CGO_ENABLED=0` 编译 Windows AMD64 版本，并输出到 `bin\proxy-subs-backend.exe`。

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

## 安全保护

错误访问保护默认开启，登录和订阅 API 使用相互独立的 IP 计数：

- 30 分钟内连续 5 次登录失败（验证码错误、账号密码错误或无效请求）后，该 IP 会被禁止登录 30 分钟。
- 30 分钟内连续 5 次访问无效 `/api/` 路径或提交错误 token 后，该 IP 会被禁止访问订阅 API 30 分钟。
- 成功登录或通过 token 校验后，会清空该 IP 对应类型的连续错误记录。
- 可从控制台右上角的“设置”进入安全设置页。关闭保护会立即清空现有错误与封禁记录；登录验证码不受该开关影响。

反向代理位于本机时，程序信任来自 `127.0.0.1` 和 `::1` 的 `X-Forwarded-For`，因此 Nginx 应保留真实客户端地址：

```nginx
proxy_set_header Host $host;
proxy_set_header X-Real-IP $remote_addr;
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Proto $scheme;
```

## 数据和升级

- 所有运行数据都在 `-db` 指定的 SQLite 文件中；请定期备份该文件。
- 网页资源在 `web/` 目录中。登录页、管理页和安全设置页分别为 `index.html`、`dashboard.html` 和 `settings.html`；`assets/` 下按公共、登录、管理和设置页面拆分样式与脚本。只修改界面时可直接替换这些文件并刷新浏览器，无需重新编译 Go 程序。
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
- `proxy-subs-backend-linux-amd64.zip`

两个平台会分别上传为独立的 Actions Artifact。压缩包都包含程序、`web/`、`static/`、README 和 LICENSE；Linux 包还包含启动和停止脚本。标签构建会同时创建 GitHub Release。

项目不会把网页资源嵌入 ELF 或 EXE，因此运行时必须保留 `web/` 目录。
