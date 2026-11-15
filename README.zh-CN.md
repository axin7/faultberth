# Faultberth

[English](README.md) | [简体中文](README.zh-CN.md)

Faultberth 是一个用 Go 编写、可自行部署的独立错误监控服务。它接收 Sentry SDK 上报的
错误 envelope，使用 SQLite 存储数据，并提供供管理员查看问题的管理界面。当前项目仍处于
早期阶段，只实现了公开 Sentry 协议的一部分，尚不能完整替代 Sentry 或 Bugsink。

## 功能

- 使用项目 ID 和密钥接收 Sentry envelope，支持 gzip 压缩。请求体及解压后数据均限制为
  1 MiB；服务在确认请求成功前持久化 envelope。
- 将错误事件归组为问题；支持按标题搜索、按状态筛选、查看事件详情与调用栈，以及解决或
  重新打开问题。已解决的问题再次出现时会自动重新打开。
- 从离线 Bugsink SQLite 快照导入项目、用户、问题、内联事件和版本，保留项目 ID、密钥、
  管理员密码哈希及原始事件。
- 单个 Go 二进制文件内嵌管理界面并使用 SQLite；运行时不需要 Node.js 或 CDN。

本仓库提供中文文档；管理界面目前仍为英文。

## 环境要求

- 快速开始需要 Go 1.25.5 或更高版本及 OpenSSL。
- 可写且私有的数据目录。Faultberth 以 `0600` 权限创建数据库；已有数据库必须是私有的
  普通文件，否则会拒绝启动。
- 远程 SDK 上报需要反向代理；访问管理界面需要 HTTPS 或 SSH 隧道。服务本身只监听回环 IP。

## 快速开始

在私有工作目录中运行以下命令。密码文件中保存了生成的管理员密码，请安全保管，以便之后登录。

```sh
git clone https://github.com/axin7/faultberth.git
cd faultberth
go build -o faultberth ./cmd/faultberth

umask 077
mkdir -p data
openssl rand -base64 32 > admin-password
./faultberth -db data/faultberth.sqlite -create-admin admin -password-file admin-password
./faultberth -db data/faultberth.sqlite -create-project 'My application'
./faultberth -db data/faultberth.sqlite -listen 127.0.0.1:18100
```

保存创建项目时输出的 `project_id` 和 `sentry_key`。在同一台机器访问
`http://127.0.0.1:18100/`，使用用户名 `admin` 和 `admin-password` 文件中的密码登录。
远程访问时，使用 SSH 隧道或仅允许管理员访问的 HTTPS 反向代理。不要通过未加密的远程连接
传输 HTTP Basic 认证凭据。

### 接入 Sentry SDK

将 SDK 的 DSN 设置为 `https://<sentry_key>@<host>/<project_id>`：其中项目 ID 和密钥
使用上一步的输出，主机使用反向代理的 HTTPS 地址。本机测试可使用 `http` 和
`127.0.0.1:18100`。以 [Sentry Go SDK](https://docs.sentry.io/platforms/go/) 为例：

```go
err := sentry.Init(sentry.ClientOptions{
    Dsn: "https://<sentry_key>@<host>/<project_id>",
})
if err != nil {
    log.Fatal(err)
}
sentry.CaptureMessage("Faultberth test event")
sentry.Flush(2 * time.Second)
```

示例需要导入 `github.com/getsentry/sentry-go`、`log` 和 `time`。发送测试事件后刷新
管理界面，确认事件已经出现。SDK 在应用中的安装和配置方式请参阅相应 SDK 文档。

### HTTP 接口

| 接口 | 用途 |
| --- | --- |
| `POST /api/<project_id>/envelope/` | 接收 envelope，使用项目密钥认证。 |
| `GET /` | 管理员问题列表，使用 HTTP Basic 认证保护。 |
| `GET /health/ready` | 数据库可访问时返回 `200 OK`，否则返回 `503`。 |

只向 SDK 客户端开放需要的 envelope 路径，保持管理界面和健康检查接口私有；在反向代理层
配置 HTTPS、请求大小限制和速率限制。

## 在 Linux 上使用 Docker

镜像基于 `scratch`，需要预先构建 Linux 二进制文件。请选择 Docker 主机的架构；如有需要，
将 `amd64` 替换为 `arm64`。先按快速开始初始化数据库和管理员，然后按 Ctrl+C 停止本地服务，再在同一 Linux 目录中运行：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o faultberth ./cmd/faultberth
tar -cf - Dockerfile faultberth | docker build -t faultberth -
sudo chown -R 14237:14237 data
docker run -d --name faultberth --restart unless-stopped \
  --network host --read-only --cap-drop ALL --security-opt no-new-privileges \
  --mount type=bind,source="$(pwd)/data",target=/data \
  faultberth -db /data/faultberth.sqlite -listen 127.0.0.1:18100
```

构建时只将 Dockerfile 和二进制文件发送给 Docker，不包含数据库和密码文件。`chown` 命令
会修改快速开始的数据目录的所有权。

容器以 UID/GID `14237` 运行，数据目录必须允许该用户写入。使用 host 网络后，主机上的
反向代理可以访问只监听回环地址的服务。请根据实际环境调整配置，并在用于生产数据前安排备份。

## 命令行参数

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `-db` | `faultberth.sqlite` | SQLite 数据库路径。 |
| `-listen` | `127.0.0.1:18100` | 服务监听的回环 IP 和端口。 |
| `-create-admin` | — | 创建管理员后退出；必须同时指定 `-password-file`。 |
| `-password-file` | — | 保存管理员密码的私有普通文件，最大 4096 字节。 |
| `-create-project` | — | 创建项目，输出项目 ID 和密钥后退出。 |
| `-import-bugsink` | — | 将 Bugsink SQLite 快照导入空数据库后退出。 |

每次调用只能选择 `-create-admin`、`-create-project` 或 `-import-bugsink` 中的一项。
密码文件不能向所属组或其他用户开放权限。运行 `./faultberth -h` 可查看内置参数说明。

## 导入 Bugsink 快照

1. 停止写入，或使用 SQLite backup API 创建一致性快照。保留快照、关联文件及旧部署，以便回滚。
2. 导入到**新的空** Faultberth 数据库，切勿将原数据库作为目标数据库：

   ```sh
   ./faultberth -db data/imported.sqlite -import-bugsink /path/to/consistent-snapshot.sqlite
   ```

3. 切换任何流量之前，核对输出的数据量、项目密钥、管理员登录、事件内容和真实 SDK 上报。
   快照创建后的变更需要重新迁移或对账；回滚时也需要处理切换后写入 Faultberth 的事件。

导入在一个事务内完成，遇到外部或无效事件载荷会拒绝导入。团队权限、可搜索的标签索引、通知、
源码映射和问题时间线不会因此成为 Faultberth 的可用功能。请保留原始数据作为归档。

## 当前限制与运维

其他 envelope 项会保留在原始 envelope 中，但不会作为独立附件处理或展示。Faultberth
不提供自动数据保留策略、速率限制、HTTPS、定时备份或账户恢复。请按需在外部配置这些能力，
监控磁盘空间，并测试 SQLite 一致性备份和恢复。替换已有服务前，应结合实际 SDK 和工作流程
评估尚缺功能及协议覆盖范围。

## 开发

```sh
go test -race ./...
go vet ./...
```

生成后的 Tailwind 样式表已提交并嵌入二进制文件。修改管理界面模板或样式后，安装
[pnpm](https://pnpm.io/) 并运行：

```sh
pnpm install --frozen-lockfile
pnpm run build:css
```

## 参考资料与许可证

传输格式与认证基于公开的 [Sentry envelope 规范](https://develop.sentry.dev/sdk/foundations/envelopes/)。
Faultberth 独立于 Sentry 和 Bugsink，采用 [MIT 许可证](LICENSE)发布。
