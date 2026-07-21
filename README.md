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

系统提示词和上下文压缩提示词分别位于 `prompts/system.md` 与 `prompts/compact.md`。Worker 在启动时读取这两个 Markdown 文件；文件缺失或内容为空会拒绝启动。路径可通过 `AGENT_SYSTEM_PROMPT_FILE` 与 `AGENT_COMPACT_PROMPT_FILE` 调整。Compose 将本地 `prompts/` 只读挂载到 Worker，修改提示词后重启 Worker 即可生效。

### 本地开发

```bash
# 启动本机开发所需的基础设施和 Nginx。中间件端口仅绑定到本机回环地址。
docker compose -f docker-compose.dev.yml up -d

# 配置浏览器直连的后端地址
cp frontend/.env.local.example frontend/.env.local

# 启动 Firecracker bridge
go run ./cmd/firecracker-bridge

# 执行数据库迁移
go run ./cmd/migrate up

# 分别启动 API 和 Worker
go run ./cmd/api      # API 服务器 :8080
go run ./cmd/worker   # 另一个终端

# 启动前端
cd frontend && pnpm install && pnpm dev
```

开发环境的 Go API 和 Next.js 分别监听宿主机 `8080`、`3000`，`docker-compose.dev.yml` 中的 Nginx 通过 `host-gateway` 连接它们，并默认在 `8081` 提供高德代理。开发页面仍从 `http://localhost:3000` 访问，避免改变高德 Web Key 的域名白名单：在根 `.env` 设置 `DEV_NGINX_HOST_PORT=8081`、`WEB_ORIGIN=http://localhost:3000` 和与 Web Key 配对的 `AMAP_SECURITY_JS_CODE`；在 `frontend/.env.local` 设置 `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080/api/v1`、`NEXT_PUBLIC_AMAP_JS_KEY` 与 `NEXT_PUBLIC_AMAP_SERVICE_HOST=http://localhost:8081/_AMapService`。修改前端公开变量后需要重启 `pnpm dev`，修改安全码后执行 `docker compose -f docker-compose.dev.yml up -d --build nginx`。

个性化设置中的地图使用高德 Web JS API 2.0。标准 Compose 部署从根目录 `.env` 读取 `AMAP_JS_KEY` 并传给 frontend 容器；Next.js 通过不缓存的 `/runtime-config.js` 在运行时把这个公开 Web Key 提供给浏览器，因此同一个 GHCR 前端镜像可用于不同部署。修改 Key 后重启 frontend 容器即可，不需要重建镜像。未设置时地图、搜索和定位不可用，文本偏好和已保存位置的文本展示不受影响。安全密钥 `AMAP_SECURITY_JS_CODE` 只能保留在 Nginx。前端在加载高德脚本前默认将 `serviceHost` 指向当前公开 origin 的 `/_AMapService`，因此必须从启用了该代理的 Nginx 入口访问应用；直接访问 Next.js 开发端口不会提供地图代理。

标准镜像启用地图需要同时完成运行时 Web Key 和 Nginx 代理配置：

1. 在运行环境的根 `.env` 设置 `AMAP_JS_KEY`。Compose 在启动 frontend 容器时传入该公开 Web Key，GHCR 镜像本身不包含任何部署方的高德 Key。
2. 在运行环境设置 `AMAP_SECURITY_JS_CODE`。Compose 只把它传给 Nginx；官方 Nginx entrypoint 启动时用 `deploy/nginx/amap-service.conf.example` 渲染 `/_AMapService` 配置，安全码不会进入前端或镜像层。
3. 使用标准 `docker compose up -d` 启动。代理只放行地图当前需要的高德路径和 GET/POST，限制请求体与每 IP 速率，并将 JSONP 响应声明为 JavaScript。安全码未设置时代理返回 `503`。

附件不会经过 Go API 传输。浏览器先分块计算 SHA-256/MD5，再向 API 申请 presigned PUT 并直接上传到 S3；长度、类型和 `Content-MD5` 都包含在签名中。上传成功并完成元数据确认后附件才进入 `ready`，发送时只携带当下已完成的附件；下载时 API 只返回 presigned GET。每个用户默认拥有 `512 MiB` 存储配额，可在用户管理中调整；存储空间 workspace 可列出、下载和删除自己的附件。API/Worker 使用私网 `S3_ENDPOINT`，浏览器使用 `S3_PUBLIC_ENDPOINT`，两者可以不同。bucket 必须允许 `WEB_ORIGIN` 发起 `PUT`、`GET`、`HEAD`，并允许 `Content-Type` 与 `Content-MD5` 请求头。

对象存储通过统一 `S3_*` 配置支持 AWS S3、阿里云 OSS 的 S3 兼容 endpoint、Cloudflare R2 和 MinIO-compatible 服务。本地 Compose 默认使用 `pgsty/minio` 社区维护镜像，`S3_PROVIDER` 仍填写 `minio`：

| Provider | `S3_PROVIDER` | Endpoint 示例 | Region 示例 |
| --- | --- | --- | --- |
| AWS S3 | `aws` | `https://s3.us-east-1.amazonaws.com` | `us-east-1` |
| 阿里云 OSS S3 兼容 | `aliyun` | `https://oss-cn-hangzhou.aliyuncs.com` | `cn-hangzhou` |
| Cloudflare R2 | `r2` | `https://<account>.r2.cloudflarestorage.com` | `auto` |
| MinIO | `minio` | `http://127.0.0.1:9000` | `us-east-1` |

`S3_AUTO_CREATE_BUCKET=true` 时 worker 会创建 bucket；CORS 必须由对象存储部署侧配置。Compose 默认对象存储服务使用 `pgsty/minio` 镜像，并通过 `MINIO_API_CORS_ALLOW_ORIGIN=${WEB_ORIGIN}` 配置 CORS；AWS、阿里云 OSS 和 R2 应在各自控制台或基础设施代码中设置对应规则。自定义 endpoint 如果需要 path-style URL，设置 `S3_BUCKET_LOOKUP=path`。Compose 内部地址与宿主机地址不同时用 `S3_DOCKER_ENDPOINT` 覆盖容器内 endpoint。未完成的上传由 worker 按 `S3_PENDING_UPLOAD_TTL`（默认 `24h`）清理。

worker 从 S3 读取用户图片后仍在 Responses API 的 `input_image.image_url` 中使用 base64 data URL；`image_generation_call.result` 的 base64 结果仍由 worker 解码并写入 S3，不使用 OpenAI Files API。导入沙箱时对象从 S3 流式写入临时路径，边传输边校验大小和 SHA-256，校验成功后再原子重命名。

### 前后端分开部署

前端镜像构建时仍通过 `NEXT_PUBLIC_API_BASE_URL` 指向浏览器可访问的后端地址，例如 `https://api.example.com/api/v1`；标准 GHCR 镜像固定使用同源 `/api/v1`。高德 Web Key 改为运行时变量：独立运行 frontend 容器时传入 `AMAP_JS_KEY`，需要覆盖代理地址时可额外传入 `AMAP_SERVICE_HOST`。这两个值由 `/runtime-config.js` 提供给浏览器。`AMAP_SECURITY_JS_CODE` 只能传给 Nginx，不得传给 frontend。后端通过 `WEB_ORIGIN` 只允许前端来源，例如 `https://app.example.com`。Next.js 不代理后端 API。

### Docker Compose 单机部署

```bash
docker compose up -d
```

默认启动 `postgres`、`redis`、`kafka`、`minio`、`migrate`、`api`、`nginx`、`frontend`、`worker`，其中 `minio` 服务使用 `pgsty/minio` 社区维护镜像。`api`、`worker`、`migrate`、`frontend` 和 `nginx` 默认从 GHCR 拉取；可使用 `ASSISTANT_IMAGE_PREFIX` 和 `ASSISTANT_IMAGE_TAG` 覆盖镜像仓库或版本。Nginx 通过 `NGINX_HOST_PORT` 发布应用入口；本地对象存储通过回环地址的 `MINIO_HOST_PORT`（默认 `9000`）发布对象 API，供浏览器直传和直下。PostgreSQL、Redis、Kafka、Go API 和 Next.js 仅在 Compose 网络内可达。浏览器默认访问 `http://localhost:8080`：Nginx 将 `/api/` 和 `/healthz` 转发给 Go API，其余路径转发给 Next.js。

单机部署到其他域名时，将域名或 TLS 入口指向 `NGINX_HOST_PORT`（默认 `8080`），并在 `.env` 中把 `WEB_ORIGIN` 设置为完整公开地址，例如 `https://assistant.example.com`。`WEB_ORIGIN` 同时用于 CORS 和邮箱验证、密码重置链接。发布的前端镜像固定使用同源 `/api/v1`；前后端分开部署时才需要另外设置 `NEXT_PUBLIC_API_BASE_URL`。Nginx 配置位于 `deploy/nginx/api.conf`。

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
