# 配置中心

一个简单稳定的分布式配置中心，支持配置的热更新推送。

## 功能特性

- **服务维度配置**：按服务组织配置，支持多环境（dev/test/prod）
- **实时推送**：SSE 长连接，配置更新后客户端秒级感知
- **后台管理**：Vue3 + Element Plus 管理界面
- **JWT 鉴权**：管理后台需要登录
- **客户端模块**：内置 Go 客户端，自动处理订阅、重连、解密与回调

## 技术栈

- **后端**：Go + Gin + GORM + JWT
- **数据库**：MySQL
- **实时通信**：SSE (Server-Sent Events)
- **前端**：Vue3 + Element Plus (CDN 版)

## 快速开始

### 1. 初始化数据库

```bash
mysql -u root -p < schema.sql
```

### 2. 修改配置

编辑 `config.yaml`：

```yaml
server:
  port: 8080

database:
  host: localhost
  port: 3306
  user: root
  password: your_password
  name: config_center

jwt:
  secret: your-secret-key
  expires: 86400
```

### 3. 运行

```bash
# 开发模式
go run .

# 或编译运行
go build -o config-center
./config-center
```

### 4. 访问后台

打开浏览器访问 `http://localhost:8080`

默认账号：
- 用户名：`admin`
- 密码：`admin123`

## API 文档

### 认证

```bash
POST /api/auth/login
{"username":"admin","password":"admin123"}

POST /api/auth/register
{"username":"user","password":"password"}
```

### 服务管理

```bash
GET    /api/services              # 获取服务列表
POST   /api/services              # 创建服务
DELETE /api/services/:id          # 删除服务
```

### 配置管理

```bash
GET    /api/configs/:service/:env # 获取配置列表
POST   /api/configs               # 创建配置
PUT    /api/configs/:id           # 更新配置
DELETE /api/configs/:id           # 删除配置
```

### 客户端接口

```bash
GET /api/client/configs/:service/:env  # 获取配置（客户端用）
GET /sse/configs?service=xxx&env=xxx&token=xxx   # SSE 订阅配置变更
```

## 客户端模块

项目内置独立 Go 客户端子模块：`config-center/client`

客户端会自动完成以下工作：

- 初始拉取配置
- 建立 SSE 长连接
- 使用 Token 自动解密推送消息
- 收到变更后自动重新拉取最新配置
- 断线自动重连

### 获取订阅 Token

1. 登录后台
2. 打开目标服务的 `Token管理`
3. 选择环境
4. 点击 `获取/创建`
5. 将生成的 `token` 配置到客户端

当前实现中，Token 同时用于：

- SSE 订阅鉴权
- SSE 推送消息解密

### 作为独立模块依赖

客户端目录下有独立的 `go.mod`，其他服务可以单独依赖这个子模块，而不需要依赖整个服务端模块。

当前子模块定义：

```go
module config-center/client
```

在其他服务中可以直接：

```bash
go get config-center/client
```

如果你后续把仓库发布到 Git 仓库，建议把客户端子模块的 `module` 改成真实仓库地址，例如：

```go
module github.com/your-org/config-center/client
```

这样其他服务就可以直接通过仓库地址依赖。

### Go 客户端示例

```go
package main

import (
    "config-center/client"
    "context"
    "log"
)

type AppConfig struct {
    Enabled bool   `json:"enabled"`
    Name    string `json:"name"`
}

func main() {
    c, err := client.New(client.Options{
        ServerURL: "http://localhost:8080",
        Service:   "order-service",
        Env:       "prod",
        Token:     "替换成后台生成的token",
        OnUpdate: func(snapshot *client.Snapshot) {
            log.Printf("配置已更新 version=%d", snapshot.Version)

            dbHost, _ := snapshot.Get("db.host")
            log.Printf("db.host=%s", dbHost)

            var app AppConfig
            if err := snapshot.DecodeJSON("app.config", &app); err == nil {
                log.Printf("app.config=%+v", app)
            }
        },
        OnError: func(err error) {
            log.Printf("配置订阅异常: %v", err)
        },
    })
    if err != nil {
        log.Fatal(err)
    }

    if err := c.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

### 只拉取一次配置

```go
c, err := client.New(client.Options{
    ServerURL: "http://localhost:8080",
    Service:   "order-service",
    Env:       "prod",
    Token:     "替换成后台生成的token",
})
if err != nil {
    panic(err)
}

snapshot, err := c.Load(context.Background())
if err != nil {
    panic(err)
}

value, ok := snapshot.Get("db.host")
_, _ = value, ok
```

### 客户端接口说明

- `client.New(options)`：创建客户端
- `Load(ctx)`：拉取一次最新配置
- `Start(ctx)`：拉取初始配置并持续订阅更新
- `Current()`：获取当前内存中的配置快照
- `Snapshot.Get(key)`：读取字符串配置
- `Snapshot.DecodeJSON(key, &target)`：读取并解析 JSON 配置

## 项目结构

```
config-center/
├── client/              # Go 客户端模块
│   ├── go.mod           # 客户端独立模块定义
│   ├── client.go        # 客户端封装
│   └── client_test.go   # 客户端测试
├── main.go              # 入口文件
├── config.go            # 配置管理
├── db.go                # 数据库连接
├── sse.go               # SSE 管理
├── handlers/            # API 处理器
│   ├── auth.go          # 认证
│   ├── service.go       # 服务管理
│   └── config.go        # 配置管理
├── middleware/          # 中间件
│   └── auth.go          # JWT 鉴权
├── models/              # 数据模型
│   └── models.go        # 模型定义
├── static/              # 静态文件
│   └── index.html       # 后台管理页面
├── schema.sql           # 数据库初始化脚本
├── config.yaml          # 配置文件
└── go.mod               # Go 模块
```

## 部署

### 单机部署

```bash
# 编译
go build -o config-center

# 运行
./config-center
```

### Docker 部署

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o config-center .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/config-center .
COPY --from=builder /app/static ./static
COPY config.yaml .
EXPOSE 8080
CMD ["./config-center"]
```

## 注意事项

1. 生产环境请修改 JWT Secret
2. 建议配置 Nginx 反向代理并启用 HTTPS
3. 数据库密码请使用强密码
4. 默认管理员账号密码请及时修改
