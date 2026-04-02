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

### 2. 配置方式

仓库内提交的是安全示例配置，不要把真实密码和密钥直接写回 Git。

推荐优先级：

1. 本地开发：复制 `config.example.yaml` 为 `config.local.yaml`
2. Docker / CI：直接使用环境变量
3. 兜底示例：保留仓库里的 `config.yaml`

程序读取顺序：

1. `CONFIG_FILE` 指定的文件
2. `config.local.yaml`
3. `config.yaml`
4. 环境变量覆盖默认值

环境变量前缀统一为 `CONFIG_CENTER_`，支持：

- `CONFIG_CENTER_SERVER_PORT`
- `CONFIG_CENTER_DB_HOST`
- `CONFIG_CENTER_DB_PORT`
- `CONFIG_CENTER_DB_USER`
- `CONFIG_CENTER_DB_PASSWORD`
- `CONFIG_CENTER_DB_NAME`
- `CONFIG_CENTER_JWT_SECRET`
- `CONFIG_CENTER_JWT_EXPIRES`

本地配置示例：

```yaml
server:
  port: 8080

database:
  host: localhost
  port: 3306
  user: root
  password: change_me
  name: config_center

jwt:
  secret: change-me-in-production
  expires: 86400
```

```bash
cp config.example.yaml config.local.yaml
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
{"username":"admin","password":"admin123","captcha_id":"xxx","captcha_code":"1234"}
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
GET /api/client/configs/:service/:env  # 获取配置（客户端用，需 Authorization: Bearer <token>）
GET /sse/configs?service=xxx&env=xxx   # SSE 订阅配置变更，需 Authorization: Bearer <token>
```

## 客户端模块

项目内置独立 Go 客户端子模块：`github.com/gudegg/disco/client`

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

- 初始拉取配置鉴权
- SSE 订阅鉴权
- SSE 推送消息解密

### 作为独立模块依赖

客户端目录下有独立的 `go.mod`，其他服务可以单独依赖这个子模块，而不需要依赖整个服务端模块。

当前子模块定义：

```go
module github.com/gudegg/disco/client
```

在其他服务中可以直接：

```bash
go get github.com/gudegg/disco/client
```

### Go 客户端示例

```go
package main

import (
    "context"
    "log"

    "github.com/gudegg/disco/client"
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
disco/
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

### Docker Compose

`docker compose` 现在默认只启动后端 `app`，数据库由你自己决定是：

- 使用你自己的 MySQL
- 或者启用项目内置的 `mysql` 服务

#### 方案一：使用你自己的 MySQL

```bash
cp .env.example .env
```

`docker compose` 会自动读取项目根目录的 `.env`，并把变量注入到 [docker-compose.yml](/E:/code_go/disco/docker-compose.yml) 中。

先修改 `.env` 中的数据库连接：

```env
APP_PORT=8080
DB_HOST=192.168.1.100
DB_PORT=3306
DB_USER=root
DB_PASSWORD=your-password
DB_NAME=config_center

MYSQL_PORT=3306
MYSQL_ROOT_PASSWORD=change_me
MYSQL_DATABASE=config_center
JWT_SECRET=change-me-to-a-long-random-string
JWT_EXPIRES=86400
```

这里的 `MYSQL_*` 在“外部数据库”模式下不会被用到，可以不用管。

启动后端：

```bash
docker compose up -d --build
```

查看状态和日志：

```bash
docker compose ps
docker compose logs -f app
```

#### 方案二：使用 compose 自带 MySQL

如果你不想单独准备数据库，可以启用 `localdb` profile：

```bash
cp .env.example .env
docker compose --profile localdb up -d --build
```

这种模式会启动：

- `app`：配置中心后端
- `mysql`：MySQL 8.0，并自动执行 `schema.sql`

使用内置 MySQL 时，建议 `.env` 保持这几项一致：

```env
DB_HOST=mysql
DB_PORT=3306
DB_USER=root
DB_PASSWORD=change_me
DB_NAME=config_center

MYSQL_PORT=3306
MYSQL_ROOT_PASSWORD=change_me
MYSQL_DATABASE=config_center
```

查看日志：

```bash
docker compose --profile localdb logs -f app
docker compose --profile localdb logs -f mysql
```

#### 常用命令

停止服务：

```bash
docker compose down
```

连同内置 MySQL 数据一起删除：

```bash
docker compose down -v
```

说明：

- `mysql` 服务只有在 `--profile localdb` 时才会启动
- 使用内置 MySQL 时，数据库文件会存到 Docker volume `mysql_data`
- `schema.sql` 只会在内置 MySQL 的数据目录第一次初始化时自动执行
- 如果你已经有旧数据，再次 `up` 不会重复导入默认管理员和表结构
- 如果要重新初始化内置数据库，执行 `docker compose down -v` 后再重新启动
- 应用容器内部通过环境变量拿配置，不依赖仓库里的 `config.yaml`

如果你只想构建镜像：

```bash
docker build -t config-center:latest .
```

单独运行容器时，直接传环境变量：

```bash
docker run -d \
  -p 8080:8080 \
  -e CONFIG_CENTER_DB_HOST=host.docker.internal \
  -e CONFIG_CENTER_DB_PORT=3306 \
  -e CONFIG_CENTER_DB_USER=root \
  -e CONFIG_CENTER_DB_PASSWORD=change_me \
  -e CONFIG_CENTER_DB_NAME=config_center \
  -e CONFIG_CENTER_JWT_SECRET=replace-with-a-long-random-secret \
  --name config-center \
  config-center:latest
```

如果你坚持使用文件配置，也可以挂载：

```bash
docker run -d \
  -p 8080:8080 \
  -e CONFIG_FILE=/app/config.local.yaml \
  -v $(pwd)/config.local.yaml:/app/config.local.yaml:ro \
  --name config-center \
  config-center:latest
```

## 注意事项

1. 不要把真实 `config.local.yaml`、`.env`、数据库密码或 JWT Secret 提交到 GitHub
2. 生产环境请修改 JWT Secret，并使用足够长的随机值
3. 建议配置 Nginx 反向代理并启用 HTTPS
4. 数据库密码请使用强密码
5. 默认管理员账号密码请及时修改
