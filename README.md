# WorkBuddy2API

**将 WorkBuddy / CodeBuddy 反向代理为通用的 OpenAI/Anthropic协议兼容 API。**

把已授权账号的模型能力转换为兼容 OpenAI Chat Completions 的接口，让支持自定义 Base URL 的客户端和应用通过同一个网关调用。配套 Web 控制台负责账号授权、运行状态、对话测试和自动任务，Docker Compose 一条命令即可启动。

> 本项目是非官方自托管网关。这里的“OpenAI 兼容”指模型列表与 Chat Completions 等已实现接口，不代表覆盖 OpenAI 的全部 API 或所有客户端功能。

## 核心功能：用 OpenAI 接口调用 WorkBuddy

- **统一接入地址**：使用网关的 `/v1` 地址和 API Key 接入客户端，不向客户端分发上游账号凭据。
- **模型列表与对话接口**：通过 `GET /v1/models` 获取模型 ID，通过 `POST /v1/chat/completions` 发起对话。
- **流式回答**：支持 Chat Completions 流式输出，适合聊天客户端和自己的应用。
- **Anthropic 文本兼容**：同一个 API Key 和模型列表也可通过 `POST /v1/messages` 调用，支持普通与流式文本对话。
- **多账号管理**：由网关维护账号池和冷却状态，网页可查看可用账号及调用情况。
- **网页辅助配置**：控制台提供 Base URL、API Key 和调用示例，并可直接测试模型回答。

![API 接入页面：Base URL、API Key 入口和调用示例](docs/superpowers/verification/2026-09-16-openai-api-access.jpg)

*API 接入截图来自本地隔离演示环境，未展示密钥。图中的 17864 是预览端口，正式部署默认使用 7863。*

### 客户端怎么填写

选择客户端的 OpenAI 兼容接口或自定义服务商入口，填写：

| 配置项 | 填写内容 |
| --- | --- |
| Base URL | `http://服务器地址:7863/v1`，公网部署建议使用自己的 HTTPS 域名 |
| API Key | 在控制台“API 接入”页面查看，不是网页登录的管理密钥 |
| 模型 | 从 `/v1/models` 获取的完整模型 ID；有 `cn:` 或 `global:` 前缀时需要保留 |

模型列表可能包含静态候选；能否实际调用取决于账号版本、权限、额度和上游状态，以真实回答为准。

### API 调用示例

先查询模型列表：

```bash
curl http://127.0.0.1:7863/v1/models \
  -H "Authorization: Bearer <你的 API Key>"
```

再使用列表中的模型 ID 发起流式对话：

```bash
curl -N http://127.0.0.1:7863/v1/chat/completions \
  -H "Authorization: Bearer <你的 API Key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<从模型列表选择的完整 ID>",
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'
```

以上是填写示例，请替换地址、密钥和模型 ID。

### Anthropic 文本接口

在控制台“API 接入”切换到 **Anthropic**，查看地址和示例；“前往对话测试”会带上所选协议与模型。官方 Python SDK 的 Base URL 填服务根地址 `http://服务器地址:7863`，SDK 会追加 `/v1/messages`，不要再追加 `/v1`。

```bash
curl -N http://127.0.0.1:7863/v1/messages \
  -H "x-api-key: <你的 API Key>" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "<从模型列表选择的完整 ID，保留 cn: 或 global: 前缀>",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "你好"}],
    "stream": true
  }'
```

`max_tokens` 必须是正整数；删除 `stream` 或设为 `false` 可获取普通 JSON 响应。支持 user/assistant 多轮消息、字符串或纯文本内容块，以及可选 `system`；不支持工具、图像、文件、扩展思考、缓存、beta、采样参数等高级字段，未支持字段会明确报错。

仅传递上游实际返回的用量，未知 token 数返回 `null`，不会估算或填 0。这与原生 API 的整数用量字段有差异，严格要求整数的客户端可能不兼容。已验证的 SDK 基线为 Python 3.12 + `anthropic==0.67.0`，不代表所有版本或客户端可用，也不代表完整支持 Claude Code；模型能力仍由 WorkBuddy / CodeBuddy 上游决定。

![Anthropic 文本接入页面](docs/superpowers/verification/anthropic-access-desktop-mock.jpg)

*截图来自隔离 mock 页面，未展示真实密钥，不代表真实模型调用验收。*

## 一条命令启动

准备一台已安装 Docker 和 Docker Compose 的 **Intel / AMD 64 位服务器（linux/amd64）**，将 [docker-compose.yml](docker-compose.yml) 保存到部署目录：

```bash
docker compose up -d
docker compose logs console
```

Compose 从阿里云仓库拉取成品镜像，**无需下载源码、构建镜像、准备 .env 或额外启动脚本**。私有镜像仓库需要先完成 Docker 登录。

1. 打开 `http://服务器地址:7863/`，使用日志中的“管理密钥”登录。
2. 进入“账号管理”，选择国内版或国际版，点击“浏览器授权”。
3. 在上游页面完成登录、扫码或验证码，再回到控制台等待结果；国际版如要求地区信息，按提示选择真实注册地区。
4. 授权成功后账号自动加载，无需重启。先在“对话测试”确认可用，再通过“API 接入”连接客户端。

登录和人工验证需要你本人完成，程序不会绕过激活或验证码。

## 配套 Web 控制台

### 对话测试

选择 OpenAI 或 Anthropic 协议、模型并发送问题，直接检查模型响应与流式显示。Anthropic 模式可设置最大输出 tokens，默认 1024；切换协议会清空当前测试对话，生成中可停止。真实部署中的测试会消耗账号额度；页面内的对话刷新后清空。

![对话测试页面：选择模型并查看流式回答](docs/superpowers/verification/2026-09-16-openai-chat-playground.jpg)

*截图使用模拟回答和演示模型列表，仅展示界面，不是某个真实模型当前可用的证明。*

### 账号与运行状态

“运行概览”展示账号总数、可用状态、冷却情况和当前请求；“账号管理”用于浏览器授权，并查看账号最近观测到的积分、调用成功与错误记录。

### 自动任务

集中查看签到、猫猫旅行、活跃上报、Token 保活、开学季、夜猫子六类任务的开关、北京时间排程和执行历史。可手动执行已启用任务，查看账号结果与脱敏日志摘要。

![桌面端自动任务执行记录与详情](docs/superpowers/verification/2026-09-15-overlay-task-console-desktop.jpg)

*截图来自隔离测试环境，包含模拟账号，不代表真实奖励到账。*

任务是否可用、账号是否符合条件以及是否获得奖励，取决于上游平台与活动规则。保活等维护任务不等同于积分奖励；页面目前用于查看与执行，不提供修改排程或开关的功能。“立即执行”覆盖全部符合条件账号，不能强行运行已禁用任务。

### 手机端查看

控制台支持窄屏布局，方便在手机上查看状态和任务。

![手机端自动任务页面](docs/superpowers/verification/2026-09-15-overlay-task-console-mobile.jpg)

*手机端测试截图；示例任务时间和开关不是所有部署的默认配置。*

## 密钥与数据

默认自动生成管理密钥、API Key 和内部通信密钥，重启后复用。管理密钥用于登录网页，API Key 用于接口调用，两者不同。

如需自定义，在 Compose 的 **core 和 console 两个服务中**填写并取消相应注释，同名密钥保持一致：

```yaml
environment:
  TZ: Asia/Shanghai
  # WB2A_ADMIN_KEY: "" # 至少 32 字节，且与 API Key 不同
  # WB2A_API_KEY: ""
```

不配置或留空的项使用自动生成的值。手动配置不改写基础密钥，取消配置后恢复基础值；默认 Compose 不从 `.env` 读取密钥。

运行数据保存在部署目录中的 `runtime/wb2api/`：

| 子目录 | 内容 |
| --- | --- |
| `auths/` | 已授权账号凭据 |
| `data/` | 账号状态与任务历史 |
| `keys/` | 自动生成的密钥 |

普通重启和容器重建不会清空这些目录。备份时保存整个目录和实际使用的 Compose 文件，不要公开上传凭据或备份。

## 使用边界

- **保护密钥**：console 启动日志会显示管理密钥，不显示 API 或内部通信密钥。不要公开分享日志或含真实密钥的 YAML。
- **公网使用 HTTPS**：通过反向代理连接 console，并在 console 的 `environment` 中添加 `WB2A_PUBLIC_ORIGIN: "https://你的域名"`，不要带路径。
- **积分仅供观察**：待确认不等于零，余额差额不等于奖励；仅将上游明确返回的奖励展示为已确认。
- **遵守平台规则**：仅使用本人授权账号，不向未授权用户开放，不绕过平台验证。模型、额度、活动及服务可用性受上游规则影响。

## 开发与来源

本项目基于 [Sliverkiss/workbuddy2api](https://github.com/Sliverkiss/workbuddy2api)，在保留上游来源和许可的基础上增加独立 Web 控制台及 Docker 部署能力。

架构、开发、测试、上游更新和镜像发布说明见 [AGENTS.md](AGENTS.md)。源码构建入口保留在 [docker-compose.build.yaml](docker-compose.build.yaml)。

遵循 [MIT License](LICENSE)，再分发时请保留原作者版权声明与许可。
