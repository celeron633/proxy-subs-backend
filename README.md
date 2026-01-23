# proxy-subs-backend

一个用 Go 语言编写的代理订阅文件服务后端。该程序提供 HTTP API 服务，用于动态下载和管理不同标签的代理订阅配置文件，支持令牌认证和 API 开关控制。

## 功能特性

- **多订阅配置管理**：支持管理多个代理订阅配置，每个配置通过唯一的标签（tag）识别
- **令牌认证**：可选的 SHA256 令牌认证机制，提高 API 安全性
- **API 开关控制**：支持动态启用/禁用 API，便于服务管理
- **灵活的文件路径**：支持 `~` 家目录展开，方便文件路径配置
- **调试模式**：支持详细的配置信息输出和日志记录
- **Gin Web 框架**：基于高性能的 Gin Web 框架

## 构建和运行

### 前提条件

- Go 1.25.6 或更高版本

### 构建

```bash
go build -o proxy-subs-backend
```

### 运行

```bash
# 使用默认配置文件 proxy-subs-backend.json
./proxy-subs-backend

# 指定自定义配置文件路径
./proxy-subs-backend -config /path/to/config.json
```

## 配置文件格式

配置文件为 JSON 格式，位置通过 `-config` 参数指定，默认为 `proxy-subs-backend.json`。

### 配置参数说明

| 参数 | 类型 | 说明 | 默认值 |
|------|------|------|--------|
| `listen_host` | string | 服务监听的主机地址 | `127.0.0.1` |
| `listen_port` | int | 服务监听的端口 | `8080` |
| `need_auth` | bool | 是否需要令牌认证 | `true` |
| `enable_api_when_start` | bool | 启动时是否启用 API | `true` |
| `debug_mode` | bool | 是否启用调试模式 | `true` |
| `token_file_path` | string | 令牌文件路径 | 见下文 |
| `subs_configs` | array | 订阅配置数组 | 见下文 |

### 订阅配置（subs_configs）

每个订阅配置包含以下字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `tag` | string | 订阅的唯一标识符，用于 API 路由匹配 |
| `file_path` | string | 订阅配置文件的本地路径（支持 `~` 展开） |
| `comment` | string | 订阅配置的注释说明 |

### 配置文件示例

```json
{
  "listen_port": 8080,
  "listen_host": "0.0.0.0",
  "need_auth": true,
  "enable_api_when_start": false,
  "debug_mode": true,
  "token_file_path": "./config/token.txt",
  "subs_configs": [
    {
      "tag": "clash-alt",
      "file_path": "/home/user/proxy/subs/clash/Configuration_wd.yaml",
      "comment": "默认订阅配置"
    },
    {
      "tag": "clash",
      "file_path": "/home/user/proxy/subs/clash/Configuration.yaml",
      "comment": "备用订阅配置"
    },
    {
      "tag": "v2ray",
      "file_path": "/home/user/proxy/subs/v2rayn/sub.txt",
      "comment": "v2ray订阅配置"
    }
  ]
}
```

## 令牌文件格式

令牌文件（由 `token_file_path` 配置指定）为纯文本文件，每行存储一个 SHA256 哈希值。

用户通过 API 传入原始令牌字符串，系统将其 SHA256 哈希后与文件中的值比对。

### 生成令牌哈希

```bash
# 方式一：使用 echo 和 sha256sum
echo -n "your_token_string" | sha256sum

# 方式二：使用 openssl
echo -n "your_token_string" | openssl dgst -sha256 -hex
```

令牌文件示例：
```
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
d4ee02c4b4a85d9d83e2f7f6f0f4f5f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0
```

## API 端点

### 1. 获取订阅配置

**请求**

```
GET /api/:apiPath?token=TOKEN
```

**参数**

| 参数 | 位置 | 类型 | 说明 |
|------|------|------|------|
| `apiPath` | 路径 | string | 订阅标签（会匹配 subs_configs 中的 tag） |
| `token` | 查询参数 | string | 认证令牌（当 `need_auth` 为 true 时必需） |

**成功响应**

- 状态码：200
- 返回对应标签的订阅配置文件内容（以文件下载方式返回）

**错误响应**

| 状态码 | 响应内容 | 说明 |
|--------|---------|------|
| 400 | `{"code": "400", "msg": "param err"}` | 缺少 apiPath 或 token 参数 |
| 400 | `{"code": "400", "msg": "invalid token"}` | 令牌无效（当需要认证时） |
| 400 | `{"code": "400", "msg": "subs not found matched tag:[tag]"}` | 未找到匹配的订阅配置 |
| 400 | `{"code": "400", "msg": "error expanding file path..."}` | 文件路径展开失败 |
| 400 | `{"code": "400", "msg": "config file for TAG [tag] not exists!"}` | 订阅文件不存在 |
| 503 | `{"code": "503", "message": "api switch is disabled"}` | API 已被禁用 |

**示例**

```bash
# 无认证
curl http://localhost:8080/api/clash

# 有认证
curl "http://localhost:8080/api/clash?token=mytoken123"

# 匹配较长的 tag（例如 apiPath 包含 "clash-alt"，会优先匹配 "clash-alt" 而非 "clash"）
curl "http://localhost:8080/api/clash-alt-backup?token=mytoken123"
```

### 2. 获取首页信息

**请求**

```
GET /
```

**响应**

```json
{
  "code": 200,
  "msg": "proxy-subs-backend"
}
```

### 3. 获取 API 开关状态

**请求**

```
GET|POST /switch/status
```

**响应**

```json
{
  "code": 200,
  "msg": "switch status",
  "data": "true|false"
}
```

### 4. 启用 API

**请求**

```
GET|POST /switch/on
```

**响应**

```json
{
  "code": 200,
  "msg": "switch enabled"
}
```

### 5. 禁用 API

**请求**

```
GET|POST /switch/off
```

**响应**

```json
{
  "code": 200,
  "msg": "switch disabled"
}
```

## 标签匹配逻辑

- 系统按 tag 长度从长到短排序（配置加载时自动执行）
- API 请求中的 `apiPath` 只要**包含**某个 tag，就会匹配对应的订阅配置
- 优先匹配较长的 tag，避免前缀冲突

示例：
- 若配置中有 tag `"clash"` 和 `"clash-alt"`，request `/api/clash-alt-backup` 会优先匹配 `"clash-alt"`

## 文件结构

```
proxy-subs-backend/
├── main.go              # 程序入口，处理命令行参数和初始化
├── server.go            # HTTP 服务器和路由定义
├── config.go            # 配置文件加载和管理
├── token.go             # 令牌管理和验证
├── api_switch.go        # API 开关控制
├── util.go              # 工具函数（路径展开等）
├── go.mod               # Go 模块定义
├── go.sum               # Go 模块依赖
├── config/
│   ├── config.json      # 服务器配置文件
│   └── token.txt        # 令牌文件
├── static/
│   └── favicon.ico      # 站点图标
└── README.md            # 本文件
```

## 依赖

- [gin-gonic/gin](https://github.com/gin-gonic/gin) - Go Web 框架

完整的依赖列表见 `go.mod` 文件。

## 常见使用场景

### 场景 1：简单的文件下载服务（无认证）

配置 `need_auth: false`，直接访问 API 下载文件。

### 场景 2：受保护的订阅服务

配置 `need_auth: true`，设置令牌文件，只有提供有效令牌的请求才能下载。

### 场景 3：临时禁用服务

通过 `/switch/off` 端点禁用 API，所有订阅请求返回 503 错误，便于维护。

## 日志和调试

启用调试模式时（`debug_mode: true`），程序会输出配置信息和请求日志，便于问题排查。

## 许可证

见 LICENSE 文件。

## 注意事项

- 确保配置文件中指定的订阅文件路径存在且可读
- 令牌文件应该包含 SHA256 格式的哈希值
- 在生产环境建议关闭 `debug_mode`
- API 开关的状态在服务重启后会重置为 `enable_api_when_start` 的配置值
