# Anthropic Text API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增隔离的 Anthropic 文本 Messages 接口与已确认的双协议 UI，不改变原 OpenAI 调用行为。

**Architecture:** core 扩展适配器在进程内调用原 OpenAI Handler，同步转换 JSON 或 SSE；console 公共代理沿用原路由，管理测试增加受会话和 CSRF 保护的入口。沿用同一个账号池、模型目录和调度链路，不改上游快照或添加服务。

**Tech Stack:** Go 标准库、既有 Go/Node/Python 测试、原生 HTML/CSS/JavaScript；Anthropic Python SDK 只进入隔离的开发验证环境。

**Spec:** `docs/superpowers/specs/2026-09-16-anthropic-text-api-design.md`（用户于 2026-09-16 确认，包括 null 用量与最小参数子集）。

## Global Constraints

- 共用现有 API Key、模型 ID、账号池、上游客户端和账号调度逻辑。
- 模型保留 `cn:` / `global:` 前缀，不增加模型别名或单独维护模型目录。
- 不支持工具调用、图像、文件、扩展思考、缓存控制及其他高级 Anthropic 功能。
- 不增加第三个服务、运行时依赖、数据库、协议自动探测或隐式删参数重试。
- 保持两个容器、一条 `docker compose up -d` 启动、既有存储与密钥机制。
- 不修改上游快照，不为历史版本增加迁移或兼容机制。
- 版本头使用 `anthropic-version: 2023-06-01`。
- 第一版必需字段为 `model`、正整数 `max_tokens`、非空 `messages`；可选字段为 `system`、`stream`。
- 未知用量返回 null 并显示未知；不能用伪造 token 数换取测试通过。
- 当前目录执行，不创建 worktree；保留无关的未跟踪 `docs/superpowers/plans/2026-09-15-docker-web-console.md`。
- 不自动更新上游、推送 Git、发布镜像或重启现有服务。测试不得访问真实账号、密钥或生产数据。
- 本文含将来执行的代码与命令，不是已运行的测试记录。

## 文件边界与顺序

| 文件 | 责任 |
| --- | --- |
| `scripts/check_anthropic_sdk.py`（新增） | 固定 SDK 的 wire-format 探针与 localhost 集成检查；非生产依赖 |
| `extensions/internal/anthropic/adapter.go`（新增） | 路由隔离、鉴权、请求转换、普通响应转换、内部会话上下文 |
| `extensions/internal/anthropic/stream.go`（新增） | SSE 响应写入器、事件转换、结束与取消 |
| `extensions/internal/anthropic/adapter_test.go`、`stream_test.go`（新增） | 表驱动协议与 streaming 行为测试 |
| `extensions/cmd/server/extension.go`、`extension_test.go` | 将适配器接到 core 模式，不改变无桥接密钥的源码模式 |
| `extensions/internal/bridge/bridge.go`、`bridge_test.go` | 新管理请求转发与会话关联 |
| `console/server.go`、`proxy.go`、`proxy_test.go` | 新受保护管理路由和新公共端点代理错误格式 |
| `console/web/index.html`、`style.css`、`app.js`、`console/web_test.cjs` | 双协议接入页、测试页和本地测试 |
| `console/browser_preview_test.go` | 真实页面的隔离 mock 预览 |
| `scripts/check.sh`、`scripts/acceptance.sh` | 新包竞态检查和真实容器 mock 验收 |
| `README.md`、`AGENTS.md`、`docs/superpowers/verification/2026-09-16-anthropic-text-api.md`（新增记录） | 用户说明、开发入口、实际验证证据 |

依赖顺序：1（SDK 契约门槛）→ 2（普通接口）→ 3（流式）→ 4（接线）→ 5（UI）→ 6（集成与交付）。每个任务独立完成红/绿验证后再进入下一项。

## 通用本地验证方式

仓库根目录没有 Go 模块。每轮修改后重新物化，禁止在旧输出目录上测试新扩展：

```bash
wb2a_round=$(mktemp -d "${TMPDIR:-/tmp}/wb2a-anthropic.XXXXXX")
python3 scripts/overlay.py prepare --output "$wb2a_round/core"
go -C "$wb2a_round/core" test ./internal/anthropic -count=1
```

任务 2 创建该包前，包不存在是预期红灯；其他环境错误不算有效红灯。记录本轮临时路径，不能删除来历不明的 `.build` 或真实 runtime。提交前 `git diff --check` 并核对暂存区，只提交该任务文件；每个任务最后保留独立本地提交，不推送。

### Task 1: 先验证官方 SDK 对兼容子集的接受度

**Files:** Create `scripts/check_anthropic_sdk.py`。

**Interfaces:** `python scripts/check_anthropic_sdk.py` 运行内存 MockTransport 契约检查；`python scripts/check_anthropic_sdk.py --url http://127.0.0.1:PORT --key legacy-api --model global:mock-model` 运行真实网关隔离检查。URL 仅允许 localhost / 127.0.0.1，key 参数仅接受测试固定值 `fixture-api` 或 `legacy-api`；默认不联网。`legacy-api` 只是现有隔离验收 fixture 的名称，不新增历史兼容逻辑。

- [ ] **1. 建立独立 SDK 环境，不更改镜像或项目运行依赖。** 固定已确认存在的版本，而非声称最新：

```bash
wb2a_sdk=$(mktemp -d "${TMPDIR:-/tmp}/wb2a-sdk.XXXXXX")
python3 -m venv "$wb2a_sdk/venv"
"$wb2a_sdk/venv/bin/python" -m pip install 'anthropic==0.67.0' 'httpx==0.28.1'
```

- [ ] **2. 写可运行的 SDK 检查入口，以 `httpx.MockTransport` 返回普通 JSON 和 SSE。** 使用以下实际消息构造；分别传入 `(3, 2)`、`(None, None)`：

```python
def message(input_tokens, output_tokens):
    return {
        "id": "msg_fixture", "type": "message", "role": "assistant",
        "model": "cn:mock-model", "content": [{"type": "text", "text": "你好"}],
        "stop_reason": "end_turn", "stop_sequence": None,
        "usage": {"input_tokens": input_tokens, "output_tokens": output_tokens},
    }

def event(kind, **fields):
    return "event: " + kind + "\ndata: " + json.dumps(
        {"type": kind, **fields}, ensure_ascii=False) + "\n\n"
```

流的开始消息从 `message(None, None)` 构造，content 置空、stop_reason 置 null；依次生成 content_block_start、两个 text_delta（你/好）、content_block_stop、message_delta（已知或 null 的完整累计用量）、message_stop。MockTransport 根据请求 JSON 的 stream 返回对应 Content-Type，并断言 SDK 请求路径、版本头、x-api-key。必须保留 unknown→known 和 unknown→unknown 两条用量路径。

- [ ] **3. 用 SDK 真正解析并断言，不只检查 JSON 字符串。** 下列断言用于各用量分支；`client` 由 `Anthropic(api_key="fixture-api", http_client=httpx.Client(transport=transport), max_retries=0)` 创建：

```python
request = dict(model="cn:mock-model", max_tokens=32,
               messages=[{"role": "user", "content": "你好"}])
reply = client.messages.create(**request)
assert reply.content[0].text == "你好"
with client.messages.stream(**request) as stream:
    assert "".join(stream.text_stream) == "你好"
    final = stream.get_final_message()
assert final.stop_reason == "end_turn"
assert final.usage.output_tokens == expected_output
assert final.usage.input_tokens == expected_input
```

额外使用 `messages.create(stream=True, **request)` 核对原始事件。先删除消息必需的 content_block_start 确认检查会失败，再恢复合法 fixture；不能通过删断言掩盖 SDK 对 null 或末尾 input_tokens 更新的限制。

- [ ] **4. 运行 `"$wb2a_sdk/venv/bin/python" scripts/check_anthropic_sdk.py`。** 预期普通、raw SSE、聚合结果三种消费均通过。若 SDK 聚合无法保留晚到或缺失用量，停止实施，带具体证据请求设计调整；不先完成后续代码再处理。
- [ ] **5. 集成模式使用同一组 SDK 断言访问隔离网关。** 文本预期为 `mock-runtime-ok`，已知用量预期为 1/1；集成模式不注入 MockTransport。记录 SDK 版本与 probe 通过不等于网关已通过。
- [ ] **6. 核对 diff，仅提交脚本：** `git add scripts/check_anthropic_sdk.py`，`git commit -m "test: add Anthropic SDK compatibility probe"`。

### Task 2: 最小普通文本适配器与严格请求校验

**Files:** Create `extensions/internal/anthropic/adapter.go`、`adapter_test.go`。

**Interfaces:**

```go
// package anthropic
func New(next http.Handler, apiKey string, maxBodyBytes int64) http.Handler
func WithConversation(ctx context.Context, id string) context.Context
```

New 仅接管 `/v1/messages`，其他路径直接交给 next。WithConversation 使用包私有 context key，仅给认证后的内部桥接注入已验证会话 ID；不能从公共私有头获取。

- [ ] **1. 写首个测试证明只新增协议入口、不改旧路径。** 使用下列 next 作为探针，发送 `{"model":"global:mock","max_tokens":32,"system":"简短回答","messages":[{"role":"user","content":[{"type":"text","text":"你好"}]}]}`，设置正确 key 和 version：

```go
next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/v1/chat/completions" { t.Fatal(r.URL.Path) }
    if r.Header.Get("Authorization") != "Bearer fixture-api" { t.Fatal("auth") }
    var body map[string]any
    if err := json.NewDecoder(r.Body).Decode(&body); err != nil { t.Fatal(err) }
    if body["model"] != "global:mock" { t.Fatal(body) }
    messages := body["messages"].([]any)
    if messages[0].(map[string]any)["role"] != "system" { t.Fatal(messages) }
    w.Header().Set("Content-Type", "application/json")
    io.WriteString(w, `{"id":"test-1","choices":[{"message":{"content":"你好"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
})
h := New(next, "fixture-api", 8<<20)
```

断言返回 content[0].text、model 前缀、end_turn、3/2 用量。单独用状态码 418 和原始 body 的 next 检查 `/v1/chat/completions`、`/v1/models` 不被改写。

- [ ] **2. 新包物化后运行 `go test ./internal/anthropic -count=1`，确认 undefined New 红灯。**
- [ ] **3. 实现最小路径。** New 返回 HandlerFunc；先验证方法、key、version/beta/query，再以 `io.LimitReader(body, limit+1)` 读取，未知字段用 `json.Decoder.DisallowUnknownFields` 拒绝，第二次 Decode 必须得到 EOF。顶层必需值用指针或 RawMessage 区分缺失/null；不要把 null 当缺省 false。system/text blocks 做嵌套字段白名单校验，文本按块顺序拼接，不人为加换行。

```go
// 转换后只产生受支持的 OpenAI 字段；不透传未经验证的请求体。
body := map[string]any{
    "model": model, "max_tokens": maxTokens,
    "messages": messages, "stream": stream,
}
// model/maxTokens/messages/stream 来自上述严格解码后的局部变量。
// 克隆请求及 URL；替换 Path/RawPath/RequestURI/Body/ContentLength，
// 删除旧长度、压缩、x-api-key、anthropic-*、桥接私有头，再设置 Bearer。
```

上述片段中的局部值不跨任务导出。认证使用 constant-time 比较；未配置 key 时失败关闭。普通输出捕获只使用生产自有 ResponseWriter，不将 httptest 导入生产包；Header/WriteHeader/Write 记录状态和有限缓冲，超过 32 MiB 返回 502 并取消底层调用。已收到的敏感上游头不复制给客户端。

- [ ] **4. 补充表驱动红/绿用例。** 逐项测试错 key/管理 key/桥接 key、缺版本/错版本/beta、非 POST、query、空 model、未知字段、max_tokens 缺失/null/小数/负数、非 bool stream、两段 JSON、嵌套未知字段、图片/工具块、messages 中 system 角色、非法 UTF-8、超限。断言拒绝路径 next 调用次数为零；正文原始及转换后都受限。

- [ ] **5. 普通响应映射覆盖 stop/length、null usage、已知真实零、缺 choices、多 choices、tool_calls、refusal、未知结束原因。** 仅接受一个文本 choice；未知/工具/拒绝返回固定安全 502，不把 reasoning_content 当正文或宣传扩展思考支持。已知上游 HTTP 状态保留，错误包统一为 `{"type":"error","error":{"type":"api_error","message":"模型调用失败，请稍后重试"}}`，401/400/413/429 分别使用 authentication_error/invalid_request_error/request_too_large/rate_limit_error；不得照搬原始错误文本。

- [ ] **6. 重物化后运行 Go test 与 vet；仅提交新适配器和测试：** `git commit -m "feat: add isolated Anthropic text request adapter"`（先精确 git add 两个文件）。

### Task 3: 增量 SSE 转换、结束判定与取消

**Files:** Create `extensions/internal/anthropic/stream.go`、`stream_test.go`；Modify `adapter.go`。

**Interfaces:** 私有响应写入器由 adapter.go 创建，直接包裹下游 ResponseWriter；不再增加服务、HTTP loopback、队列或后台 pump。

```go
type streamWriter struct {
    dst http.ResponseWriter
    header http.Header
    cancel context.CancelFunc
    model, id, stopReason string
    pending []byte
    status int
    started, done bool
    inputTokens, outputTokens *int64
    err error
}
func newStreamWriter(w http.ResponseWriter, model string, cancel context.CancelFunc) *streamWriter
func (s *streamWriter) Header() http.Header
func (s *streamWriter) WriteHeader(status int)
func (s *streamWriter) Write(p []byte) (int, error)
func (s *streamWriter) Flush()
func (s *streamWriter) finish() error
```

- [ ] **1. 测试 next 分多次 Write 输出一个含中文的 OpenAI SSE 帧，然后等待测试 channel 才输出剩余。** 客户端必须在释放 channel 前收到首个 text_delta。用 `httptest.NewServer` + `bufio.Reader` 的真实 HTTP 流验证，不能只用最终 Recorder 内容证明实时。

```go
chunks := []string{
    `data: {"id":"x","choices":[{"index":0,"delta":{"content":"你"}}]}` + "\n\n",
    `data: {"id":"x","choices":[{"index":0,"delta":{"content":"好"}}]}` + "\n\n",
    `data: {"id":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}` + "\n\n",
    "data: [DONE]\n\n",
}
// 对每个字符串按单字节 Write，覆盖 JSON、UTF-8、换行的任意边界。
// 收集 event 与 data.type，断言顺序和内容均一致，只有一次 message_stop。
```

- [ ] **2. 运行新测试，确认 stream 分支当前不能通过。**
- [ ] **3. 实现 Write 的有界分帧及事件状态机。** 累积未完成帧，支持 LF/CRLF、多行 data 与注释；单帧上限 8 MiB，超限中止。Flush 只能刷新已转换事件，不能发送半帧。第一条有效数据触发 message_start 和内容块；ID 生成一次（crypto/rand），模型使用请求中的完整 ID。未知用量开始为 null；解析 actual usage 后更新最后累计值，不作累加。

```text
等待首帧 → 文本块开始 → 文本增量 → 收到 stop/length → 等待可选 usage / [DONE]
                                                       ↓
                        内容块结束 → message_delta → message_stop
任意状态遇到解析/写入/上游错误 → 取消 → error（仅连接可写时）→ 不再正常完成
```

next 返回后调用 finish；只有可信 finish_reason 才允许正常结束。`[DONE]` 或 EOF 没有 finish_reason 都是截断错误；finish_reason 后不能再有文本增量。HTTP 非 2xx 切到有界错误消费，不能先发送 200。错误状态不可逆，后续 Write 返回 error，cancel 必须触发请求 Context。

- [ ] **4. 增加异常用例并逐个红/绿。** 覆盖空流、只有 `[DONE]`、只有 role、无终止原因、畸形 JSON、tool_calls、content_filter、上游 error 帧、oversized frame、重复终止、跨块 UTF-8、下游断开。fake next 等待 `<-r.Context().Done()`，用测试超时确认取消传播；不依赖 sleep 判断完成。
- [ ] **5. SDK 探针事件契约与 adapter 输出对齐；Go 普通/流式返回内容和 usage 语义一致。** 重新物化后运行 `go test -race ./internal/anthropic -count=1` 与 `go vet ./internal/anthropic`。
- [ ] **6. 精确暂存 adapter.go、stream.go、stream_test.go 与本任务更改的 adapter_test.go，提交** `feat: stream Anthropic text events with cancellation`。

### Task 4: core 接线与受保护的网页入口

**Files:** Modify `extensions/cmd/server/extension.go`、`extension_test.go`、`extensions/internal/bridge/bridge.go`、`bridge_test.go`、`console/server.go`、`proxy.go`、`proxy_test.go`、`scripts/check.sh`。

**Interfaces:** 公共 `/v1/messages`；管理 `POST /admin/messages` → `POST /internal/v1/messages`。管理请求 envelope 为 `{conversation_id, request}`，其中 request 是完整 Anthropic 文本请求体，不包含额外私有字段。

- [ ] **1. 扩展已有路由测试，新增以下断言并观察红灯：** 新公共路由使用 x-api-key；新管理路由需要登录+CSRF+同源；bridge key 不能直接调用公共接口；伪造 x-console/x-bridge 头无效；OpenAI 的 Bearer 和请求内容不变。
- [ ] **2. core 只在已有 `key == ""` 返回之后包裹 Handler：**

```go
public = anthropic.New(public, cfg.APIKey, int64(cfg.Server.MaxBodyMB)<<20)
// 下面原有 bridge.Config{Public: public} 与 mux.Handle("/", public) 保持原顺序。
```

保留 `TestCoreSourceModeRemainsUnchanged`；源码模式不新增目录或改变原 handler。bridge.Config 增加 MaxBodyBytes 并由同一个 cfg.Server.MaxBodyMB 传入，用于读取管理 envelope，不另设环境变量。

- [ ] **3. bridge 新路由使用 `h.withOwner(h.messages)`。** 新私有方法 `func (h *handler) messages(w http.ResponseWriter, r *http.Request)` 有界解码 envelope，conversation_id 限制 1～128 字符且仅 `[A-Za-z0-9_-]`，拒绝额外字段和尾随 JSON；验证 request 不超限。新请求设置 x-api-key 和固定 version，删除桥接 Authorization/Owner，将 conversation 注入 `anthropic.WithConversation`。adapter 转换后再写入 OpenAI body 的 conversationId。原 `forward` 不变，防止影响旧管理接口。

- [ ] **4. console 路由表增加一行：**

```go
{"POST /admin/messages", "POST", "/internal/v1/messages"},
```

复用原 management/withAdmin。proxy ErrorHandler 只对公共 `/v1/messages` 返回 Anthropic 503 错误包，其他路径保持现有 adminError；确保公共伪造私有头仍被 stripPrivateHeaders 删除。

- [ ] **5. 运行既有+新测试：** 重物化后 `go test ./cmd/server ./internal/bridge ./internal/anthropic -count=1`；`go -C console test -race ./...`。验证 envelope 超限、无 owner、恶意会话 ID、伪造上下文、logout 取消、请求原对象未被修改。
- [ ] **6. scripts/check.sh 现有 race 包列表增加 `./internal/anthropic`；精确暂存本任务文件并提交** `feat: wire Anthropic API and protected console testing`。

### Task 5: 落实用户已确认的双协议 UI

**Files:** Modify `console/web/index.html`、`style.css`、`app.js`、`console/web_test.cjs`、`console/browser_preview_test.go`。

**Interfaces:** 沿用 `api`、`jsonAPI`、`clearChat`、`history`、`conversation`、`activeRequest`。增加 `let protocol = 'openai'`、`setProtocol(value)`、`renderAccess()`；只共享必要的协议选择，不引入前端框架或通用 provider 抽象。

- [ ] **1. 用既有 taskFixture/logoutFixture 补充测试，先验证默认及切换行为。** 对实际 DOM 新增 access-model、两个协议按钮组、max-tokens、API endpoint/示例、前往测试按钮；测试 mock 元素同步补最小所需方法。

```javascript
const {ctx,get}=taskFixture(()=>new Promise(()=>{}));
vm.runInContext("history=[{role:'user',content:'old'}];setProtocol('anthropic')",ctx);
assert.equal(vm.runInContext('history.length',ctx),0);
assert.equal(get('base-url').value,'http://console.test');
assert.match(get('api-example').textContent,/anthropic-version/);
assert.equal(get('effort').hidden,true);
```

- [ ] **2. 执行 `node --test console/web_test.cjs`，确认新函数/DOM 行为未实现而失败。**
- [ ] **3. 实现协议切换与接入页。** 原有字体、颜色与布局复用；协议按钮 aria-pressed，native input max-tokens 默认为1024、min1、step1。`setProtocol` 在 activeRequest 时立即返回；值相同时不清空；否则 clearChat、更新两个组、更新 Base URL/端点/鉴权/示例及 effort/max 显隐。渲染真实 modelList 到 access-model，无模型时禁用复制示例和测试跳转。复制示例由 JSON.stringify 生成且不嵌入真实 key。登出恢复默认协议并清空敏感字段。

- [ ] **4. 新请求使用管理 envelope，旧 OpenAI 分支原样保留：**

```javascript
const anthropicBody={model:$('model').value,
  max_tokens:Number($('max-tokens').value),
  messages:outgoing,stream:true};
const response=protocol==='anthropic'
  ? await api('messages',{conversation_id:conversation,request:anthropicBody},controller.signal)
  : await api('chat',body,controller.signal);
```

仅在 Anthropic 分支校验 max_tokens 是安全正整数；上游模型的真实最大值仍由上游决定，不把原型8192上限带入产品。沿用 history 的 role/content 即可，不能加入 reasoning_effort。发送前快照当前协议，禁止生成过程中所有协议按钮、两个模型选择及清空；finally 恢复后运行 updateEfforts 避免错误启用不支持的档位。

- [ ] **5. 复用 SSE 分帧循环，在 processFrame 内分支解析。** Anthropic 读取 content_block_delta.delta.text、message_delta.usage、message_stop；error 显示安全错误并取消；未知用量用 `?? '—'`，不是 `|| 0`。没有 message_stop 不写成功 history。旧 OpenAI `[DONE]`、reasoning 和工具展示保持原逻辑。

- [ ] **6. 补充红/绿用例：** 真实 /admin/messages URL与body、分片事件、多轮、跨协议清空、停止、错误、截断、unknown/0用量、登录过期、生成时切换禁止、access→chat携带协议模型、没模型禁用、模型刷新不覆盖正在生成的选择。执行所有 Node 与 console Go 测试。
- [ ] **7. 扩展 browser_preview_test.go 的 mock core 新管理路由，返回明确的模拟 SSE。** 用既有浏览器预览检查 1280 与 390 像素宽度、两种协议、页面导航与停止；截图只含测试数据。视觉结果与用户确认的 visualize 保持一致，但不把演示说明、Tweak控件、假密钥硬编码进产品。
- [ ] **8. 精确暂存本任务五个文件，提交** `feat: add dual-protocol access and chat UI`。

### Task 6: 容器、SDK、文档及完整回归

**Files:** Modify `scripts/acceptance.sh`、`README.md`、`AGENTS.md`；Create `docs/superpowers/verification/2026-09-16-anthropic-text-api.md`；必要时按已有规则保存真实 mock 页面截图至同目录。

**Interfaces:** 复用 acceptance 的 global:mock-model、legacy-api、隔离端口、项目名和清理机制。SDK脚本任务1的 `--url` 模式用于本任务实际运行，不是仅重跑契约 fixture。

- [ ] **1. 在原 OpenAI mock_stream 断言之后加入新端点非流式与流式验收。** 使用已有 `$legacy_port` 和 legacy-api，而非读取真实 runtime：

```bash
anthropic_stream=$(curl --noproxy '*' -fsS \
  -H 'x-api-key: legacy-api' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{"model":"global:mock-model","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}' \
  "http://127.0.0.1:$legacy_port/v1/messages")
[[ "$anthropic_stream" == *'mock-runtime-ok'* ]]
[[ "$anthropic_stream" == *'event: message_stop'* ]]
[[ "$anthropic_stream" != *'data: [DONE]'* ]]
```

非流式响应用 Python json 断言文本、end_turn及1/1 token；HTTP401/400 和正确的错误包同时验证；增加 /admin/messages 的会话和 CSRF 场景。现有原 OpenAI 验收不得删减。

- [ ] **2. 为 SDK 集成提供可选测试入口。** acceptance 在清理前，如 `WB2A_SDK_PYTHON` 已配置，则执行下面代码；该变量仅测试脚本使用，不进入 Compose 或镜像：

```bash
if [[ -n "${WB2A_SDK_PYTHON:-}" ]]; then
  "$WB2A_SDK_PYTHON" "$repo_root/scripts/check_anthropic_sdk.py" \
    --url "http://127.0.0.1:$legacy_port" --key legacy-api --model global:mock-model
fi
```

- [ ] **3. 执行全量检查并记录真实结果。**

```bash
python3 -m unittest discover -s scripts -p 'test_*.py' -v
python3 -m unittest discover -s deploy -p 'test_*.py' -v
bash scripts/check.sh
WB2A_SDK_PYTHON="$wb2a_sdk/venv/bin/python" bash scripts/acceptance.sh
docker compose --env-file /dev/null -f docker-compose.yml config --quiet
git diff --check
```

保持现有 build 代理的显式用法，不默认添加代理。Docker网络或依赖安装失败应报告环境阻塞，不能宣称验收通过。隔离测试的清理由原脚本负责，不停止其他项目容器。

- [ ] **4. README 新增简洁功能与 curl 示例：** `POST /v1/messages`、x-api-key、version、max_tokens、cn/global完整ID；注明纯文本范围、null用量偏差、普通/流式、非完整Claude Code支持。不扩大为支持所有 Anthropic API/客户端。AGENTS 增加 adapter、管理envelope、SDK验证命令和测试边界，不重复复制整份设计。
- [ ] **5. 写验证记录。** 记录SDK版本、测试命令/退出结果、mock与真实上游边界、桌面窄屏截图、上游与Compose未改、未发布/未部署。不是已经执行的步骤不能写成通过。
- [ ] **6. 核对 diff 仅含授权实现，精确暂存本任务文件，提交** `test: verify Anthropic gateway and document text compatibility`。最终汇报新增端点、兼容限制、实际验证及未推送/未发布/未部署状态；保留无关工作区文件。

## 计划自检与停止条件

- 设计第1～3节：任务2、4；响应/SSE/取消：任务1～3；UI全部交互：任务5；验收与文档：任务6。
- SDK null/晚到用量不通过时先停止在任务1，不能删断言、虚构token或改变用户已确认契约。
- `New`、`WithConversation`、`streamWriter`、管理envelope和UI函数名称在任务间一致。
- 若必须修改上游、增加运行依赖、变更部署架构或扩大协议范围，先与用户确认。
- 执行选择由用户决定；计划编写阶段不启动实现或子代理。

## 版本依据

- [Anthropic Python SDK 0.67.0](https://pypi.org/project/anthropic/0.67.0/)：用于固定验收基线，不代表最新版本，也不承诺其他版本均兼容。
- [SDK streaming helpers](https://github.com/anthropics/anthropic-sdk-python/blob/main/helpers.md)：raw events、text_stream 与最终消息聚合需要分别验证。
