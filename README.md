# assistant

Agentic AI 对话助手，基于 OpenAI Responses API，支持网络搜索、文件理解、图片生成，以及基于 microVM 沙箱的不受信任命令安全执行。

## 功能展示

点击截图可查看原图。

<table>
  <tr>
    <td width="50%" valign="top">
      <a href="./assets/websearch.png">
        <img src="./assets/websearch.png" alt="联网检索与执行时间线：对话主区域展示检索进度，右侧面板展示完整执行步骤" />
      </a>
      <br />
      <strong>联网检索与执行时间线</strong>
      <br />
      <sub>在对话中持续反馈检索进度，并通过时间线查看完整工具调用过程。</sub>
    </td>
    <td width="50%" valign="top">
      <a href="./assets/sandbox.png">
        <img src="./assets/sandbox.png" alt="沙箱执行：在隔离环境中运行大整数分解程序并返回结构化结果" />
      </a>
      <br />
      <strong>隔离沙箱执行</strong>
      <br />
      <sub>在 Firecracker 沙箱中执行不受信任命令并返回结果。</sub>
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <a href="./assets/latex.png">
        <img src="./assets/latex.png" alt="Markdown 与 LaTeX：对话中展示傅里叶变换公式和数学推导" />
      </a>
      <br />
      <strong>Markdown 与 LaTeX</strong>
      <br />
      <sub>流式呈现结构化 Markdown、代码、表格和数学公式。</sub>
    </td>
    <td width="50%" valign="top">
      <a href="./assets/billing.png">
        <img src="./assets/billing.png" alt="用量与计费：设置界面展示账户余额、资金流水和模型用量" />
      </a>
      <br />
      <strong>用量与计费</strong>
      <br />
      <sub>查看账户余额、资金流水、模型用量和逐次请求成本。</sub>
    </td>
  </tr>
</table>

## 部署

### 环境准备

```bash
cp .env.example .env
# 编辑 .env，至少配置认证、存储和 agent 提示词参数
# 生产环境必须将 WEB_ORIGIN 设置为用户实际访问的 HTTPS 地址，例如 https://assistant.example.com
# 生成 provider credential 主密钥：openssl rand -base64 32
# 将结果写入 PROVIDER_CREDENTIAL_MASTER_KEY；部署后必须保持不变
# 启动后通过系统管理界面创建 provider credential、模型和已发布价格，并设置默认模型
```

系统提示词和上下文压缩提示词分别位于 `prompts/system.md` 与 `prompts/compact.md`。Worker 和合并模式 Backend 在启动时读取这两个 Markdown 文件；文件缺失或内容为空会拒绝启动。路径可通过 `AGENT_SYSTEM_PROMPT_FILE` 与 `AGENT_COMPACT_PROMPT_FILE` 调整。Compose 将本地 `prompts/` 只读挂载到 Worker，修改提示词后重启 Worker 即可生效。

### 本地开发

```bash
# 启动基础设施；开发 override 仅将中间件端口绑定到本机回环地址
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d postgres redis kafka minio

# 配置浏览器直连的后端地址
cp frontend/.env.local.example frontend/.env.local

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
cd frontend && pnpm install && pnpm dev
```

前端开发服务器不代理 API。复制的 `frontend/.env.local.example` 将 `NEXT_PUBLIC_API_BASE_URL` 设置为 `http://localhost:8080/api/v1`；本地开发后端时应将 `WEB_ORIGIN` 设置为 `http://localhost:3000` 以允许跨域访问。未设置 `NEXT_PUBLIC_API_BASE_URL` 时，前端源码默认使用同源 `/api/v1`。

### 前后端分开部署

前端镜像构建时通过 `NEXT_PUBLIC_API_BASE_URL` 指向浏览器可访问的后端地址，例如 `https://api.example.com/api/v1`。该值会进入浏览器 bundle，修改后必须重新构建前端。后端通过 `WEB_ORIGIN` 只允许前端来源，例如 `https://app.example.com`。Next.js 不代理任何后端 API。

### Docker Compose 单机部署

```bash
docker compose up -d --build
```

默认启动 `postgres`、`redis`、`kafka`、`minio`、`migrate`、`api`、`nginx`、`frontend`、`worker`。只有入口 Nginx 向宿主机发布端口，PostgreSQL、Redis、Kafka、MinIO、Go API 和 Next.js 仅在 Compose 网络内可达。浏览器默认访问 `http://localhost:8080`：Nginx 将 `/api/` 和 `/healthz` 转发给 Go API，其余路径转发给 Next.js。前端使用同源相对地址 `/api/v1`，Nginx 对 SSE 路径关闭压缩、缓存和代理缓冲。宿主机映射端口可通过 `NGINX_HOST_PORT` 调整。

单机部署到其他域名时，将域名或 TLS 入口指向 `NGINX_HOST_PORT`（默认 `8080`），并在 `.env` 中把 `WEB_ORIGIN` 设置为完整公开地址，例如 `https://assistant.example.com`。`WEB_ORIGIN` 同时用于 CORS 和邮箱验证、密码重置链接。Compose 会固定构建同源 `/api/v1`；前后端分开部署时才需要另外设置 `NEXT_PUBLIC_API_BASE_URL`。Nginx 配置位于 `deploy/nginx/api.conf`。

Compose 不包含独立部署的 CubeSandbox 集群，也不包含必须在宿主机运行的 Firecracker bridge。每个 Worker 进程默认提供 4 个 request slot，但只建立一个 Kafka group consumer；同一 conversation 在分区稳定期间固定命中同一进程。

若镜像构建或容器内 Worker 访问外部服务超时，而宿主机访问正常，需配置 `.env` 中 `DOCKER_HTTP_PROXY` / `DOCKER_HTTPS_PROXY`。该地址必须同时可被 BuildKit 和运行中的容器访问。

### 沙箱生命周期

沙箱在没有命令活动一段时间后会自动进入 `stopped`，下次创建、执行命令或导入附件时自动恢复。Firecracker 停止 VM 进程但保留可写 rootfs；CubeSandbox 对 MicroVM 执行 pause 并保留快照。超过 stopped 保留时间后，沙箱会先进入可重试的 `releasing`，provider 确认删除后再进入 `destroyed`。时间均可通过环境变量调整：

```bash
SANDBOX_IDLE_STOP_AFTER=15m
SANDBOX_STOPPED_RETENTION=24h
SANDBOX_REAPER_INTERVAL=1m
SANDBOX_REAPER_BATCH_SIZE=20
SANDBOX_COMMAND_DEFAULT_TIMEOUT=30s
SANDBOX_COMMAND_MAX_TIMEOUT=5m
```

### PVM / CubeSandbox 沙箱部署

应用通过 CubeAPI 和 CubeProxy 使用 CubeSandbox；PVM 是 CubeSandbox 计算节点上的 KVM 后端，不直接暴露给应用。当前适配基于 CubeSandbox `v0.5.1`。PVM 节点要求 x86_64、root 权限、定制 host/guest kernel 和重启；如果宿主机已经提供原生 `/dev/kvm`，应优先使用原生 KVM。

1. 按 [CubeSandbox PVM deployment](https://github.com/TencentCloud/CubeSandbox/blob/v0.5.1/docs/guide/pvm-deploy.md) 安装 PVM host kernel，加载 `kvm_pvm`，并以 `CUBE_PVM_ENABLE=1` 安装 CubeSandbox。
2. 创建包含 envd 和 `/workspace` 的 READY 模板。
3. 将 CubeAPI、CubeMaster、Cubelet、Redis、MySQL 和 WebUI 放在私网；CubeAPI 必须启用 `AUTH_CALLBACK_URL`，CubeProxy 只允许 API/Worker 访问。
4. 初期将 Cubelet 的 `host.quota.paused_resource_release_ratio` 设为 `0`，保证 paused sandbox 可以恢复后删除。
5. 配置 API 与 Worker：

```bash
SANDBOX_PROVIDER=cubesandbox
SANDBOX_CUBE_API_URL=http://cube-api.internal:3000
SANDBOX_CUBE_API_KEY=your-private-api-key
SANDBOX_CUBE_TEMPLATE_ID=tpl-xxxxxxxx
SANDBOX_CUBE_PROXY_NODE_IP=10.0.0.12
SANDBOX_CUBE_PROXY_PORT_HTTP=80
SANDBOX_CUBE_PROXY_SCHEME=http
SANDBOX_CUBE_DOMAIN=cube.app
SANDBOX_CUBE_CLUSTER_ID=production
SANDBOX_CUBE_ALLOW_INTERNET=false
SANDBOX_CUBE_DENY_OUT=0.0.0.0/0
SANDBOX_EXEC_ENABLED=true
```

Assistant 是生命周期的唯一控制方：创建时会向 CubeSandbox 传递 `NeverTimeout`，空闲 pause 和超期 destroy 仍由本项目的 reaper 执行。CubeSandbox 当前不能直接删除 paused sandbox，runtime 会先恢复再删除。CubeSandbox create API 尚不支持服务端 `Idempotency-Key`；runtime 会将 conversation 和 request key 写入远端 metadata 便于排障，但生产环境仍需监控并清理响应丢失产生的孤立沙箱。

用户附件不会预先复制到沙箱。模型只在任务需要时按附件 ID 调用 `sandbox.import_attachment`；Worker 校验附件属于当前 conversation、大小和 SHA-256 后，将内容写入 `/workspace/attachment-<attachment-id><ext>`。对象存储 key 和凭据不会传入沙箱，单文件上限为 128 MiB，且该工具与 `sandbox.exec` 一样受 `SANDBOX_EXEC_ENABLED` 控制。

切换默认 provider 时不要立即删除旧 provider 配置。`Manager` 会按数据库中保存的 provider 路由，必须等旧 Firecracker 沙箱全部进入 `destroyed` 后才能停止 bridge。

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

Firecracker rootfs 必须包含与当前代码一同构建的 `sandbox-agent`，以提供附件写入端点；guest 环境中的 `SANDBOX_AGENT_MAX_FILE_BYTES` 应至少为 `134217728`。

### 数据库迁移

```bash
go run ./cmd/migrate up       # 执行所有未应用迁移
go run ./cmd/migrate down     # 回滚最近一次迁移
go run ./cmd/migrate version  # 查看当前迁移版本
```

部署 `000006_sandbox_lifecycle` 前应先停止旧版本 Worker 并等待正在执行的沙箱命令结束，再执行迁移和启动新版本 Worker。回滚前必须确保没有 `stopped` / `releasing` 沙箱或执行租约。部署 `000009_remove_agentbay_provider` 前必须先通过旧版本终止所有 AgentBay Session 并将对应记录标记为 `destroyed`；迁移会在仍有未销毁的 AgentBay sandbox 时拒绝执行。回滚 `000010_add_cubesandbox_provider` 前必须先销毁所有 CubeSandbox sandbox，并将其历史记录归档到 `sandboxes` 表之外或删除；否则迁移会拒绝执行。

## 开源协议

本项目基于 [Apache License 2.0](LICENSE) 开源。
