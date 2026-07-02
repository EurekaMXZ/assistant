# assistant

Agentic AI 对话助手，基于 OpenAI Responses API，支持网络搜索、文件理解、图片生成，以及基于 Firecracker microVM 沙箱的安全执行。

## 技术栈

### 后端

| 组件 | 技术 |
|------|------|
| 语言 | Go 1.26 |
| HTTP 框架 | Gin |
| 数据库 | PostgreSQL 18 |
| 消息队列 | Apache Kafka 3.9 (Kraft) |
| 实时推送 | Redis Pub/Sub |
| 对象存储 | MinIO (S3 API) |
| 容器化 | Docker + Docker Compose |
| 沙箱执行 | Firecracker microVM |
| VM 通信 | vsock (AF_VSOCK) + HTTP bridge |
| VM 网络 | TAP 设备 + Linux bridge + iptables NAT |

### 前端

| 组件 | 技术 |
|------|------|
| 框架 | Next.js 16 (App Router) |
| UI | React 19 + Tailwind CSS 4 |
| 组件库 | shadcn/ui + Base UI |
| Markdown | react-markdown + KaTeX + rehype-highlight |
| 表单 | react-hook-form + zod |

## 架构

```
                         ┌──────────┐
                         │ Frontend │
                         │ Next.js  │
                         └─┬───┬────┘
                    HTTP   │   │  SSE
                    REST   │   │
           ┌───────────────┘   └─────────────────┐
           ▼                                     ▼
┌──────────────────────┐            ┌───────────────────────┐
│     API (Gin) :8080  │            │    Redis 7 Pub/Sub    │
│                      │◀───────────│      (流式事件扇出)     │
│  auth / conversations│            └───────────────────────┘
│  /messages /turns    │
└──────────┬───────────┘
           │  turn.accepted / turn.context_ready
           │  context.compaction.requested
           ▼
┌──────────────────────────────────────────────────┐
│              Kafka (Kraft, 16 partitions)        │
│              durable workflow event bus          │
└──────────────────────┬───────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────────────────────────┐
│                 Worker (conversation affinity, 4 request slots)     │
│                                                                      │
│  ┌──────────────────────┐   ┌───────────────────────────────────┐    │
│  │   Context Cache      │   │         WorkflowEngine            │    │
│  │   (ring buffer)      │   │                                   │    │
│  │                      │   │  OutboxRelay │ StaleTurnRequeuer  │    │
│  │   anchor + tail      │   │  ContextCompactor                 │    │
│  └──────────┬───────────┘   └──────────────┬────────────────────┘    │
│             │                              │                         │
│             └──────────┬───────────────────┘                         │
│                        ▼                                             │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │                      TurnRunner                               │    │
│  │                                                              │    │
│  │  HandleAccepted ──▶ HandleContextReady ─▶ turn_run.requested  │    │
│  │       │                    │                                 │    │
│  │       ▼                    ▼                                 │    │
│  │  ContextLoader     ToolOrchestrator (one request per event)   │    │
│  │                         │                                    │    │
│  │               ┌─────────┼─────────┐                          │    │
│  │               ▼         ▼         ▼                          │    │
│  │         List Tools   LLM 请求  Execute Tools                 │    │
│  │         (catalog)        │          │                        │    │
│  └──────────────────────────┼──────────┼────────────────────────┘    │
│                             │          │                             │
└─────────────────────────────┼──────────┼─────────────────────────────┘
                              │          │
                    ┌─────────┘          └─────────────────────────┐
                    ▼                                               │
┌──────────────────────────────┐                                    │
│    OpenAI Responses API      │                                    │
│    (streaming)               │                                    │
└──────────────────────────────┘      ┌─────────────────────────────┼──────────────────────────┐
                                      ▼                             ▼                          ▼
                         ┌────────────────────┐  ┌──────────────────────┐  ┌──────────────────────────────┐
                         │   Tavily Search    │  │    Conversation      │  │          Sandbox             │
                         │   search/extract   │  │       rename         │  │                              │
                         │                    │  │                      │  │  ┌──────────┐ ┌────────────┐ │
                         └────────────────────┘  └──────────────────────┘  │  │  HTTP    │ │ Firecracker│ │
                                                                          │  │  Client  │ │  Bridge    │ │
                                                                          │  │          │ │            │ │
                                                                          │  │ app side │ │  HTTP REST │ │
                                                                          │  │          │ │ :8787      │ │
                                                                          │  └──────────┘ └─────┬──────┘ │
                                                                          └─────────────────────┼────────┘
                                                                                                │
                                                                        ┌───────────────────────┘
                                                                        ▼
                                                         ┌──────────────────────────────┐
                                                         │    firecracker-bridge        │
                                                         │    宿主机守护进程              │
                                                         │    监听 127.0.0.1:8787        │
                                                         │                              │
                                                         │  POST   /sandboxes            │
                                                         │  DELETE /sandboxes/{id}       │
                                                         │  POST   /sandboxes/{id}/exec  │
                                                         │                              │
                                                         │  VM 生命周期:                  │
                                                         │  分配 CID → 启动 VMM          │
                                                         │  配置 boot/drive/vsock        │
                                                         │  InstanceStart → 等待 agent   │
                                                         │                              │
                                                         │  可选网络:                     │
                                                         │  fcbr0 bridge + iptables NAT  │
                                                         │  每 VM: TAP + guest IP        │
                                                         └──────────────┬───────────────┘
                                                                        │
                                                    ┌───────────────────┼───────────────────┐
                                                    │ vsock             │                   │
                                                    │ CONNECT 52\n      │  TAP / iptables   │
                                                    ▼                   │  (可选出站网络)    │
                                          ┌──────────────────┐          │                   │
                                          │  Firecracker VMM │          │                   │
                                          │  (microVM)       │◀─────────┘                   │
                                          │                  │                              │
                                          │  vCPU: 1         │                              │
                                          │  mem: 512 MiB    │                              │
                                          │  rootfs: rw ext4 │                              │
                                          │                  │                              │
                                          │  ┌────────────┐  │                              │
                                          │  │sandbox-    │  │                              │
                                          │  │agent :52   │  │                              │
                                          │  │            │  │                              │
                                          │  │ /health    │  │                              │
                                          │  │ /exec      │  │                              │
                                          │  │ /network/  │  │                              │
                                          │  │  configure │  │                              │
                                          │  └────────────┘  │                              │
                                          └──────────────────┘                              │
                                                                                            │
┌──────────────────────────────────────────────────────────────────────────────────────────┘
│
│   ┌─────────────────────┐   ┌─────────────────────┐   ┌─────────────────────┐
│   │    PostgreSQL 18    │   │      Redis 7        │   │    MinIO (S3)       │
│   │                     │   │                     │   │                     │
│   │  users              │   │  Pub/Sub 扇出        │   │  请求/响应 Blob      │
│   │  conversations      │   │  (流式 token 增量)    │   │  上下文锚点           │
│   │  turns              │   │                     │   │  附件文件             │
│   │  messages           │   │                     │   │                     │
│   │  outbox_events      │   │                     │   │                     │
│   │  turn_runs          │   │                     │   │                     │
│   │  tool_calls         │   │                     │   │                     │
│   │  attachments        │   │                     │   │                     │
│   │  sandboxes          │   │                     │   │                     │
│   │  turn_stream_events │   │                     │   │                     │
│   └─────────────────────┘   └─────────────────────┘   └─────────────────────┘
│
│   沙箱工具条件展示（internal/tool/catalog.go）:
│   • sandbox.create  ── 会话无活跃沙箱
│   • sandbox.destroy ── 会话有活跃沙箱
│   • sandbox.exec    ── 有活跃沙箱 且 SANDBOX_EXEC_ENABLED=true
│
│   每会话最多一个活跃沙箱。rootfs 可读写，命令间文件状态持久保留。
│   统一通过 HTTPRuntime 访问 Firecracker bridge。
└───────────────────────────────────────────────────────────────────────────────

```

## 项目结构

```
├── cmd/                      # 入口（7 个二进制）
│   ├── api/                  # API 服务器
│   ├── worker/               # Kafka 消费者 + 工作流引擎
│   ├── backend/              # 合并 API + Worker（单进程）
│   ├── migrate/              # 数据库迁移
│   ├── password-hash/        # 密码哈希 CLI
│   ├── firecracker-bridge/   # Firecracker HTTP 桥接守护进程
│   └── sandbox-agent/        # Firecracker VM 内 vsock 代理
├── internal/                 # 核心应用逻辑（22 个包）
│   ├── bootstrap/            # 依赖注入 / 运行时组装
│   ├── config/               # 环境变量配置
│   ├── domain/               # 领域模型和常量
│   ├── server/               # HTTP 路由、SSE 流
│   ├── auth/                 # JWT 认证
│   ├── llm/                  # 模型客户端接口
│   ├── openai/               # OpenAI Responses API 客户端
│   ├── tool/                 # 工具目录、执行器、处理器
│   ├── workflow/             # 工作流引擎（Turn runner、Compactor、Outbox）
│   ├── postgres/             # 数据库仓库
│   ├── kafka/                # Kafka 生产者/消费者
│   ├── redis/                # Redis Pub/Sub
│   ├── minio/                # MinIO 客户端
│   ├── sandbox/              # Firecracker bridge HTTP 客户端
│   ├── firecrackerbridge/    # Firecracker VM 生命周期管理
│   ├── sandboxagent/         # VM 内代理（vsock 监听 + 命令执行）
│   ├── cache/                # 上下文环形缓冲缓存
│   ├── tavily/               # Tavily 网页搜索 API 客户端
│   ├── billing/              # 模型用量计费
│   ├── stream/               # 流事件类型定义
│   └── attachment/           # 文件附件服务
├── frontend/                 # Next.js 前端
├── db/migrations/            # PostgreSQL 首发基线迁移
├── docs/                     # 文档
│   └── API.md                # HTTP API 参考
├── docker-compose.yml
├── Dockerfile
├── .env.example
└── LICENSE
```

## 部署

### 环境准备

```bash
cp .env.example .env
# 编辑 .env，至少配置认证、存储和 agent 提示词参数
# 生成 provider credential 主密钥：openssl rand -base64 32
# 将结果写入 PROVIDER_CREDENTIAL_MASTER_KEY；部署后必须保持不变
# 启动后通过系统管理界面创建 provider credential、模型和已发布价格，并设置默认模型
```

### 本地开发

```bash
# 启动基础设施
docker compose up -d postgres redis kafka minio

# 启动 Firecracker bridge（后端仅保留 Firecracker 沙箱）
go run ./cmd/firecracker-bridge

# 执行迁移
go run ./cmd/migrate up

# 启动后端（API + Worker 合并模式）
go run ./cmd/backend

# 或分别启动
go run ./cmd/api      # API 服务器 :8080
go run ./cmd/worker   # Worker

# 启动前端
cd frontend && npm install && npm run dev
```

### Docker Compose 后端部署

```bash
docker compose up -d --build
```

默认启动 `postgres`、`redis`、`kafka`、`minio`、`migrate`、`api`、`worker`，不包含前端或必须在宿主机运行的 Firecracker bridge。每个 Worker 进程默认提供 4 个 request slot，但只建立一个 Kafka group consumer；同一 conversation 在分区稳定期间固定命中同一进程。

若容器内 Worker 访问外部 API 超时而宿主机正常，需配置 `.env` 中 `DOCKER_HTTP_PROXY` / `DOCKER_HTTPS_PROXY` 指向容器可达的代理地址。

### Firecracker 沙箱部署

Firecracker bridge 必须运行在宿主机上（需要 `/dev/kvm`、TAP 设备、iptables 权限），API 与 Worker 都通过 HTTP 与它通信。

```bash
# 1. 准备 Firecracker 内核与 rootfs 镜像
# 2. 启动 bridge
export FIRECRACKER_BIN=firecracker
export FIRECRACKER_KERNEL_IMAGE=/path/to/vmlinux
export FIRECRACKER_ROOTFS_IMAGE=/path/to/rootfs.ext4
export FIRECRACKER_BRIDGE_ADDR=127.0.0.1:8787
export FIRECRACKER_BRIDGE_TOKEN=your-secret-token   # 可选
export FIRECRACKER_NET_ENABLED=true                  # 可选：启用 VM 网络
go run ./cmd/firecracker-bridge

# 3. 在 .env 中配置 API / Worker 使用 bridge
SANDBOX_BRIDGE_URL=http://host.docker.internal:8787
SANDBOX_BRIDGE_TOKEN=your-secret-token
SANDBOX_EXEC_ENABLED=true
```

### 迁移命令

`000001_initial_schema` 是首发基线迁移，部署时从全新空数据库开始。

```bash
go run ./cmd/migrate up       # 执行所有未应用迁移
go run ./cmd/migrate down     # 回滚最近一次迁移
go run ./cmd/migrate version  # 查看当前迁移版本
```

## 开源协议

本项目基于 [Apache License 2.0](LICENSE) 开源。
