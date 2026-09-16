# AI 开发指南

本文面向在本仓库工作的 AI 编程助手，维护架构、开发、验证、上游更新与镜像发布约定。`README.md` 面向使用者，主打 **将 WorkBuddy / CodeBuddy 反向代理为 OpenAI 兼容 API**；Web 控制台是配套能力，不要把 README 写成开发手册。

## 优先遵守

- 当前处于开发阶段，以当前设计为准，不为历史版本新增兼容层、迁移分支或双轨配置。
- 不直接修改 `upstream/`。新增 core 能力放 `extensions/`，修改上游既有文件通过 `patches/`，控制台直接改 `console/`。
- 生产部署保持 `docker compose up -d` 一条命令启动，不新增宿主启动脚本、独立 init 服务或服务器端构建依赖。
- 生产 Compose 的 `environment` 默认只有 `TZ` 和注释形式的可选密钥；固定内部参数内置在镜像。不重新引入 `.env` 插值或必填密钥。
- 保留相对目录挂载、源码构建入口和 `linux/amd64` 发布架构，不新增 Docker 健康检查。
- 修改前检查 `git status --short`，保留用户已有改动；不能为通过验证而重置工作区、清空数据或覆盖真实配置。
- 更新上游、发布镜像、推送 Git、重启实际服务分别需要对应授权。发布镜像不等于部署授权。

## 架构

```text
浏览器 / OpenAI 或 Anthropic 文本客户端
            │ :7863
            ▼
console（独立 Go 服务，内嵌 HTML/CSS/JavaScript）
  ├─ /admin/*：管理会话、CSRF、网页操作
  │                 └─ 桥接密钥 → core /internal/v1/*
  └─ /v1/*、/status、/healthz：公共接口代理 → core
                                                    │
                                  同一个账号池、上游客户端、调度器
                                                    │
                                          WorkBuddy / CodeBuddy
```

- **core**：固定上游加扩展和补丁，负责账号池、公共 API、OAuth、任务执行及持久化。不得为控制台另建账号池或调度器。
- **console**：负责网页、管理认证和代理；不加载账号凭据，不直接执行签到等业务。
- 仅 console 发布宿主端口。core 只在 Compose 网络内访问；console 仅只读挂载密钥目录，不挂载账号和状态目录。
- 当前 README 重点介绍 `/v1/models`、`/v1/chat/completions` 与流式调用；不得把“OpenAI 兼容”宣传为完整覆盖所有 OpenAI API 或客户端功能。
- `/v1/messages` 由 core 的 Anthropic 文本适配器在进程内复用原 OpenAI Handler；不另建账号池、HTTP 回环或协议代理服务。只在启用 core 桥接的分支包装，未启用桥接的源码模式保持原 Handler。

## 目录与代码入口

| 路径 | 职责 |
| --- | --- |
| `upstream/`、`upstream.lock` | 上游普通源码快照；锁文件记录来源、commit 和源码摘要。 |
| `extensions/cmd/server/extension.go` | core 初始化、密钥、桥接和任务生命周期接线。 |
| `extensions/internal/bridge/` | 内部管理接口，复用公共能力。 |
| `extensions/internal/anthropic/` | Messages 文本请求、普通响应与增量 SSE 适配，传递取消与真实用量。 |
| `extensions/internal/oauth/`、`extensions/internal/pool/` | 授权流程、账号热加载等扩展。 |
| `extensions/internal/scheduler/`、`extensions/internal/taskrun/` | 任务目录、执行观察、单运行器和持久历史。 |
| `extensions/scripts/` | Python 任务结果事件与测试。 |
| `patches/series`、`patches/README.md` | 补丁顺序、修改原因、验证方式和移除条件。 |
| `console/main.go`、`console/server.go`、`console/proxy.go` | 配置、管理会话、路由与代理。 |
| `console/web/`、`console/web_test.cjs` | 无前端框架的页面资源及 Node 测试；通过 Go embed 打包。 |
| `deploy/` | Dockerfile、core 镜像启动入口、配置和隔离验收工具。 |
| `scripts/overlay.py` | 源码物化、补丁标识和上游更新。 |
| `scripts/check.sh`、`scripts/acceptance.sh` | 本地检查和真实容器隔离验收。 |
| `scripts/release.sh`、`scripts/release.py` | 时间戳镜像发布入口与实现。 |
| `docs/superpowers/verification/` | 验证记录、界面截图；模拟素材不代表真实上游结果。 |

## 上游与补丁机制

唯一上游为 `https://github.com/Sliverkiss/workbuddy2api`。实际版本以 `upstream.lock` 为准，不在多份文档中重复写死 commit。

`scripts/overlay.py prepare` 的步骤：校验源码摘要 → 复制快照到新目录 → 复制扩展文件 → 按 `patches/series` 执行 `git apply --check` 并应用补丁。不会改写 `upstream/`。

1. 新文件和测试放到 `extensions/` 的对应相对路径；不能通过扩展覆盖已有上游文件。
2. 需要改既有上游文件时，在物化目录验证后维护补丁，同时更新补丁说明和必要测试。
3. `.build/` 是临时产物。不能只改物化代码而不回写扩展或补丁。
4. `console/` 是独立 Go 模块，不向上游公共 Handler 注入网页管理实现。
5. 上游提供等价行为且相关回归测试无需补丁也通过时，才移除对应补丁。

### 更新上游

仅在明确授权后执行；构建相关目录须无未提交改动，显式指定 commit 或 tag：

```bash
python3 scripts/overlay.py update --ref COMMIT_OR_TAG
git diff -- upstream upstream.lock
git diff -- patches extensions deploy console scripts
```

脚本在候选目录验证补丁、扩展、测试及隔离容器验收；成功才更新快照和锁文件。失败保留诊断候选，不自动提交、推送、发布或重启服务。不要绕过摘要检查、强行应用失败补丁或手改锁文件掩盖不一致。

## 开发与测试

开发机需要 Git、Bash、Python 3、Go、Node.js、curl、Docker Compose 和 Buildx。Go 最低版本以各模块 `go.mod` 为准，镜像使用 Go 1.23；竞态测试需要支持 CGO 的本机编译环境。部署成品镜像的服务器不需要这些开发工具。

以下命令从仓库根目录执行。根目录不是 Go 模块，不要直接在根目录运行 `go test ./...`。

```bash
python3 -m unittest discover -s scripts -p 'test_*.py' -v
python3 -m unittest discover -s deploy -p 'test_*.py' -v
bash scripts/check.sh
docker compose --env-file /dev/null -f docker-compose.yml config --quiet
git diff --check
```

`check.sh` 在新临时目录物化 core，运行 Go 测试、vet、关键包竞态测试、console 竞态测试、Node 页面测试和 Python 任务测试。

定点调试时使用尚不存在的输出路径：

```bash
python3 scripts/overlay.py prepare --output .build/core-review
go -C .build/core-review test ./internal/bridge ./internal/taskrun
go -C console test -race ./...
node --test console/web_test.cjs
```

修改后重新物化，旧物化目录不会自动同步；重复运行使用新的输出路径，不删除不明来源的目录。

涉及镜像、Compose、启动参数、密钥或持久化时，还需运行 `bash scripts/acceptance.sh`。它在隔离项目、端口和存储中验证直接启动、YAML 密钥覆盖与取消覆盖、日志、时区、数据保留以及 mock API/任务流程。禁止将测试指向真实凭据或数据；模拟验收不证明真实上游授权成功或奖励到账。

### Anthropic 文本验证

公共 `/v1/messages` 使用同一个模型 API Key（`x-api-key`）和固定 `anthropic-version: 2023-06-01`。`New(next, apiKey, maxBodyBytes)` 仅截获该路径，其他 OpenAI 路径不变；未知用量保持 `null`，不得伪造零或宣称完整 Claude Code 兼容。

网页 `POST /admin/messages` 沿用管理会话、同源、CSRF 和退出取消，代理至带 owner 的 `/internal/v1/messages`。请求 envelope 为 `{conversation_id, request}`；ID 限 `[A-Za-z0-9_-]{1,128}`，request 为完整 Anthropic 文本请求，不加私有字段。bridge 有界读取并验证 envelope，通过 `WithConversation` 注入可信上下文，由 adapter 写入 OpenAI `conversationId`；不接受公共私有头伪造会话。体积限制沿用 `server.max_body_mb`，包含管理 envelope。

SDK 仅用于隔离测试，不进入镜像或生产依赖。使用 Python 3.12 临时虚拟环境安装 `anthropic==0.67.0 httpx==0.28.1` 后执行：

```bash
# 内存契约：未知/晚到用量、raw events、text_stream、最终消息聚合
/path/to/venv/bin/python scripts/check_anthropic_sdk.py
# 实际访问验收脚本创建的隔离 mock 网关；执行后沿原机制清理
WB2A_SDK_PYTHON=/path/to/venv/bin/python bash scripts/acceptance.sh
```

验收保留 OpenAI 流式断言，并覆盖 Anthropic JSON、SSE、错误包和管理入口；`WB2A_SDK_PYTHON` 未设置时仅跳过 SDK 步骤，不能将这次运行报告为 SDK 通过。实际网关 SDK 验收与内存 fixture、真实上游调用分开记录。Python 3.14 不作为 SDK 0.67.0 验收环境；不得为绕过解析失败而删断言。

新增行为先补能失败的回归测试，再做最小修改。纯文档改动核对事实、命令和链接即可，不因此重建发布镜像。只报告有实际执行结果的验证。

### 界面截图

可复用现有隔离预览，不接触真实账号、容器或上游服务：

```bash
WB2A_BROWSER_PREVIEW=1 go -C console test -run '^TestAdminBrowserPreview$' -v -timeout 20m
```

预览地址和测试登录方式以该测试输出为准；使用完关闭预览进程。图片保存到 `docs/superpowers/verification/`，README 用相对路径引用并标明模拟环境。不要展示真实密钥、账号信息，或通过修改 DOM 伪造成功响应。

## Docker 与运行配置

- `docker-compose.yml`：只拉取成品，两个镜像同时间戳、`linux/amd64`，对外端口 `0.0.0.0:7863:7863`。
- `docker-compose.build.yaml`：继承运行版，以本地 `:dev` 标签构建，只读挂载 `deploy/default-config.json`。两种入口共享数据目录，不应同时启动。
- 两个服务 YAML 保留 `TZ: Asia/Shanghai` 和可选密钥注释。core Dockerfile 内置 core 模式、监听地址、账号/状态路径；console 内置 core 地址、监听地址和密钥文件路径。
- core 入口调整三个挂载根目录为 UID/GID 10001、权限 700，再降权 exec 业务进程；不递归改写既有文件。console 以普通用户运行。
- 生产不定义 Docker 健康检查；console 等待 core 生成共享密钥。`/livez` 表示存活，空账号时 `/healthz` 可以返回 503。

| 宿主相对路径 | 容器路径 | 用途 |
| --- | --- | --- |
| `./runtime/wb2api/auths` | `/app/auths` | core 账号凭据 |
| `./runtime/wb2api/data` | `/app/data` | core 状态和任务历史 |
| `./runtime/wb2api/keys` | `/run/wb2a` | core 写入，console 只读 |

密钥缺失或为空时，使用自动生成并持久化到 `runtime/wb2api/keys/keys.json` 的基础密钥。YAML 覆盖不改写基础文件，取消覆盖恢复基础值；两个服务的同名覆盖必须一致。

管理和桥接密钥至少 32 字节，API Key 非空，三者不同；非法配置应失败，不静默替换。按产品要求，console 启动日志显示有效管理密钥，但禁止输出 API/桥接密钥。不创建或回写 `.env`，不修改宿主或其他容器的环境变量。

维护 `.gitignore`、`.dockerignore` 的边界，不提交或打包真实密钥、配置、账号、备份、`runtime/`。公网代理需在 console 的 `environment` 指定 `WB2A_PUBLIC_ORIGIN` 为完整 HTTPS origin（不带路径）；不得关闭同源、CSRF、会话归属或桥接认证。

需要自定义业务配置时，保存到 `runtime/wb2api/config.json`，通过显式只读挂载启动：

```bash
docker compose -f docker-compose.yml -f deploy/compose.config.yml up -d
```

该命令需要实际部署授权。缺失路径、目录或非法 JSON 拒绝启动；后续操作保持相同的 `-f` 参数。API Key 覆盖必须由两个服务共享，不能只改 core 的 JSON。

## 账号与任务语义

- 六类任务 ID 为 `checkin`、`travel`、`activity`、`keepalive`、`school`、`cat`。网页读 core 调度器的目录，不维护第二份开关或排程。
- 手动运行覆盖全部符合条件账号，不能强行启动禁用任务，也不能扩展成任意 shell 执行入口。
- 保留单运行器、幂等请求与持久历史语义；历史损坏不能绕过记录继续调度。
- 未知积分或奖励不能变成零；余额差额、脚本退出码不能证明到账，只采集上游明确的奖励事件。
- OAuth 登录、扫码、验证码、必要的地区选择由用户完成，不绕过验证或激活。
- 账号更新先持久化再发布到现有账号池，保留凭据并发快照与账号状态，避免旧刷新覆盖新授权。
- 管理会话、公共 API Key、内部桥接密钥是不同权限，不能混用。

## 构建与发布

仅构建本地镜像、不启动服务：

```bash
docker compose -f docker-compose.build.yaml build
```

成品镜像发布到下列固定仓库，每次使用同一个新的 Unix 秒级时间戳：

```text
registry.cn-hangzhou.aliyuncs.com/cateyes/go:wb2api-core-<timestamp>
registry.cn-hangzhou.aliyuncs.com/cateyes/go:wb2api-webui-<timestamp>
```

获得发布授权、核对测试并提交本次构建输入后执行：

```bash
docker login registry.cn-hangzhou.aliyuncs.com
bash scripts/release.sh
```

复用已有 Docker 登录，不读取或输出凭据文件。发布脚本：检查标签未占用 → 测试 → 构建两个 amd64 镜像 → 隔离验收 → 推送 → 回拉比对镜像 ID → 原子更新 Compose 标签。

- 不跳过验收，不复用已发布时间戳，不维护 `latest`，串行发布。
- 网络或鉴权错误不等于标签不存在。部分推送失败不自动删标签，查明原因后用新时间戳重试。
- 回拉检查使用当前登录身份，不代表匿名拉取可用；不得擅自修改仓库公开权限。
- 成功后审阅并提交 Compose 标签，交付版本和验证结果；脚本不推送 Git、不部署服务器。
- 当前仅发布 `linux/amd64`，Apple Silicon 可模拟运行，不得误发 ARM 镜像。
- 保留 LICENSE 和上游来源；镜像中不包含用户数据。

确有需要时显式传入构建代理，例如本机 Docker Desktop：

```bash
WB2A_BUILD_HTTP_PROXY=http://host.docker.internal:7890 \
WB2A_BUILD_HTTPS_PROXY=http://host.docker.internal:7890 \
bash scripts/release.sh
```

`WB2A_BUILD_*` 也适用于 `scripts/acceptance.sh`，仅传入构建步骤。不要把此地址设置为宿主 HTTP_PROXY/HTTPS_PROXY，或硬编码为项目默认值；Docker 后台拉基础镜像的代理由 Docker 自身管理。

## 交付

README 用中文介绍功能和使用，AGENTS 维护技术事实。版本以 Compose 和 `upstream.lock` 为准，避免在文档重复写死。不得在文档工作中顺便删 `.github/`、用户数据或其他历史文件。

交付时说明修改点、实际验证、是否发布/部署，以及保留的无关工作区改动；未做的操作不声称已完成。
