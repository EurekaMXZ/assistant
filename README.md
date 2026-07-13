# assistant

Agentic AI 对话助手，基于 OpenAI Responses API，支持网络搜索、文件理解、图片生成，以及基于 Firecracker microVM 沙箱的安全执行。

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

# 启动 Firecracker bridge
go run ./cmd/firecracker-bridge

# 执行数据库迁移
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

### 阿里云 AgentBay 沙箱部署

后端可通过官方 AgentBay Go SDK 直接创建云端 Agent Sandbox，不需要启动 Firecracker bridge。AgentBay session 使用手动释放生命周期，与 conversation sandbox 的 `active` / `destroyed` 状态保持一致；不再使用时应调用 sandbox destroy，避免继续占用云端资源。

```bash
SANDBOX_PROVIDER=agentbay
AGENTBAY_API_KEY=your-agentbay-api-key
AGENTBAY_REGION_ID=cn-hangzhou
AGENTBAY_IMAGE_ID=code_latest
AGENTBAY_POLICY_ID=                 # 可选：AgentBay 安全策略 ID
AGENTBAY_API_TIMEOUT=1m
SANDBOX_EXEC_ENABLED=true
```

`AGENTBAY_REGION_ID` 支持 AgentBay 当前区域，默认 `cn-hangzhou`；`AGENTBAY_IMAGE_ID` 默认 `code_latest`。切换 provider 后，数据库中已有 sandbox 仍按其持久化的 `provider` 路由：存在 active Firecracker sandbox 时需保留 `SANDBOX_BRIDGE_URL`，存在 active AgentBay sandbox 时需保留 `AGENTBAY_API_KEY`。

### 数据库迁移

```bash
go run ./cmd/migrate up       # 执行所有未应用迁移
go run ./cmd/migrate down     # 回滚最近一次迁移
go run ./cmd/migrate version  # 查看当前迁移版本
```

## 开源协议

本项目基于 [Apache License 2.0](LICENSE) 开源。
