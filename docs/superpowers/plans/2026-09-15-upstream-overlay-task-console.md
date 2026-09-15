# Upstream Overlay and Task Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将现有可用的 Docker/Web 定制迁入独立扩展和显式补丁，保持固定上游快照纯净，并增加可追溯的自动任务页面。

**Architecture:** 当前仓库保留历史，`upstream/` 保存固定源码，构建时在临时目录注入扩展、应用补丁。独立 console 对外提供页面与代理，core 持有唯一账号池、OAuth 流程、scheduler 和任务记录。先通过现有功能及模拟数据迁移验收，再接入任务执行与页面。

**Tech Stack:** Go 标准库、现有 upstream Go 依赖、原生 HTML/CSS/JavaScript、Node 内置测试、Python 3 标准库构建工具与上游既有脚本、Docker Compose；不新增数据库、前端框架或队列服务。

**Spec:** `docs/superpowers/specs/2026-09-15-upstream-overlay-task-console-design.md`

## Global Constraints

- 所有相对路径以 `/Users/zeelin/WorkCode/workbuddy2api` 为根；不是当前终端可能所在的 institute-report。先完整读取 Spec 和当前仓库适用技能。
- 当前目录实施，不创建 worktree；保留 `.git` 历史、用户 `.agents/`、环境文件及无关改动。原 origin 不是定制代码推送目标；本计划不创建远端、不推送、不发布镜像。
- 初始上游是 `https://github.com/Sliverkiss/workbuddy2api` 的 `c576b489fa22e3c156e960ee6336c4e653a0d95c`，此次不升级业务版本。
- 普通源码快照，不用 submodule；保留许可证、源文件内容和可执行模式；禁止嵌套 `.git`、真实配置、账号与运行数据进入源码快照/镜像层。
- `upstream/` 构建前后不变；修改旧文件只能进有序补丁，新增扩展不能覆盖同名文件；不允许忽略校验、冲突或测试失败。
- `console` 是唯一宿主机入口，默认 7863；两个服务非 root，不挂 Docker socket；公共 `/v1`、`/status` 不注入特权密钥。
- 首次 Compose 构建允许联网取依赖；`docker compose up -d` 不升级上游；源码更新后使用 `docker compose up -d --build`。
- 管理密钥、公共 API Key、桥接密钥独立；只有 core 接触上游凭据；管理会话与 OAuth 临时流程允许重启失效，已保存账号不丢失。
- 六类任务：`checkin`、`travel`、`activity`、`keepalive`、`school`、`cat`；默认北京时间分别为 9/21、9/21、10、22、12、1 点。有效配置和下次执行时间来自同一 scheduler。
- 第一版只查看配置、立即执行全部符合条件账号、查看最近记录。禁用任务不能手动运行，不做开关/时间编辑、单账号选择、停机补跑。
- 唯一 scheduler；后台任务全局串行，聊天不全局停机；手动异步、幂等，同任务计划撞车合并，不同任务计划撞车等待。
- 历史保留最近 30 天且最多 1000 条；单条日志摘要最多 64 KiB，截断有标记；运行中记录不清理；无数据库服务。
- 状态区分 `running/success/partial_failure/failed/skipped/interrupted/unknown`；未知余额/奖励为 null；余额变化不等于任务奖励，退出码 0 不等于业务成功。
- 开始记录不能持久化就不执行；结束持久化失败只告警，不重试业务；损坏历史不覆盖且不影响公共模型 API。
- 验证使用模拟上游/账号/独立卷，不进行真实领奖、人机验证或账户消费。实际容器切换前检查在途操作；不删除卷，不让新空卷冒充迁移完成。
- 实施前读当前代码完整调用链；先写失败测试再实现。代码片段是关键契约与测试起点，迁移优先复用既有实现，不重写账号池、排程算法或活动规则。

## 文件地图与迁移账本

| 路径 | 责任与来源 |
|---|---|
| `upstream/`、`upstream.lock` | 原始 Git commit 导出的普通文件；锁文件记录格式版本、URL、SHA、源码摘要 |
| `scripts/overlay.py`、`scripts/test_overlay.py` | 快照摘要、无覆盖物化、补丁校验、候选更新；Python 标准库与 Git CLI |
| `scripts/check.sh`、`scripts/acceptance.sh` | Go/JS/脚本验证及隔离 Compose 模拟验收 |
| `patches/series`、`patches/README.md` | 补丁顺序及每项目的、基线、路径、测试、移除条件 |
| `patches/0001-auth-pool-consistency.patch` | 当前 auth/pool 安全修复，新增 install 文件放 extensions |
| `patches/0002-upstream-credential-snapshots.patch` | 当前 upstream 包凭据快照、重授权、待激活出站保护 |
| `patches/0003-core-wiring.patch` | main/config/HTTP 的最小扩展接线、CLI 共用 OAuth、必要兼容修复 |
| `patches/0004-scheduler-observation.patch` | 统一调度接点、既有业务观察点、受控脚本执行与上下文 |
| `patches/0005-regression-tests.patch` | 必须修改原测试文件的适配；说明现有 ledger 时间断言修正 |
| `extensions/internal/oauth/` | 移入当前 OAuth client 与测试，不复制 token 到 console |
| `extensions/internal/pool/install.go`、`install_test.go` | 移入当前账号热安装及回归 |
| `extensions/internal/upstream/reauthorize_test.go` | 移入当前重授权/激活回归 |
| `extensions/internal/bridge/bridge.go`、`oauth.go`、对应 `_test.go` | 内部版本化路由、密钥校验、脱敏状态及 core-owned 授权流程 |
| `extensions/cmd/server/extension.go`、`extension_test.go` | 同 package main 复用 Config，初始化密钥、包装原 Handler、连接已有实例 |
| `extensions/internal/scheduler/console.go`、`console_test.go` | 复用私有 nextFire 的任务目录、同步执行、结果收集和脚本凭据快照 |
| `extensions/scripts/task_events.py`、`test_task_events.py` | 脚本结构化结果的受限输出；不实现活动算法 |
| `extensions/internal/taskrun/store.go`、`runner.go`、对应 `_test.go` | 原子有界记录、幂等、串行化、计划合并与重启恢复 |
| `console/go.mod`、`main.go`、`server.go`、`proxy.go`、对应 `_test.go` | 独立 Go module `workbuddy2api-console`；移动管理会话、同源/CSRF、公共代理 |
| `console/web/`、`console/web_test.cjs`、`console/browser_preview_test.go` | 迁移现有资产/浏览器夹具，新增任务列表、详情、触发、分页 |
| `deploy/core.Dockerfile`、`deploy/console.Dockerfile`、`deploy/migrate.py`、`deploy/test_migrate.py` | 两个镜像、离线迁移规划/备份，运行初始化实现放 extension.go |
| `deploy/compose.acceptance.yml` | 仅模拟验收的独立项目/卷，不引用真实数据或真实上游 |
| `docker-compose.yml`、`.dockerignore`、`.gitignore`、`README.md` | 单入口默认启动、允许跟踪新文档、使用与维护指南 |
| `docs/superpowers/verification/2026-09-15-overlay-migration.md` | 迁移映射、测试证据、实际卷与备份位置；不写密钥正文 |

现有根目录 `cmd/`、`internal/`、Go module、原上游业务脚本的删除仅在 Task 9，必须先逐项映射到 upstream/extension/patch 并验证；不能因为未跟踪就丢弃。根 `scripts/` 将只保留定制工具，原脚本仍在 `upstream/scripts/`。构建产物放忽略的 `.build/`，备份使用仓库外权限受限目录。

## 跨任务契约

### 构建与版本

`upstream.lock` 使用 JSON，导出基线后按以下代码生成内容，不手填摘要：

```python
lock = {
    "format": 1,
    "repository": "https://github.com/Sliverkiss/workbuddy2api",
    "commit": "c576b489fa22e3c156e960ee6336c4e653a0d95c",
    "source_sha256": source_digest(root / "upstream"),
}
(root / "upstream.lock").write_text(json.dumps(lock, indent=2) + "\n", encoding="utf-8")
```

`patches/series` 一行一个文件名，空行和 `#` 注释可忽略。`patch_identity` 是扩展清单摘要、series 内容、有序补丁内容拼接后的 SHA-256；不包含真实配置。

### HTTP 桥接（版本 1）

- 内部固定前缀 `/internal/v1/`；`Authorization: Bearer <bridge-key>`，OAuth 还必须携带由 console 生成的 `X-Console-Owner`，不能转发浏览器自报的 owner。
- `GET info` → `{protocol:1, upstream_commit:string, patch_identity:string, global_enabled:boolean}`。
- `GET status` → 当前脱敏账号/网关状态；不返回 Auth 对象。
- `POST oauth`、`GET oauth/{id}`、`POST oauth/{id}/region` 延续现有管理端请求与安全响应字段；`DELETE owners/{owner}/flows` 取消退出会话所有授权流程。
- `GET models`、`POST chat` 只为已鉴权管理员服务，core 在内部调用原 Handler 时使用 core 持有的 API Key；不返回该 key。
- `GET tasks`、`POST tasks/{taskId}/runs`、`GET task-runs`、`GET task-runs/{runId}` 分别映射设计中的 `/admin/` 路由。
- 所有管理调用先确认协议版本恰为 1；不匹配返回 503 和中文提示。公共 API 不依赖管理协议是否匹配。
- 错误体统一 `{error:string, run_id?:string}`；401/403/400/404/409/503 按 Spec。日志只写固定错误分类和请求/运行 ID，不回传原始上游响应。
- `POST .../runs` 请求 `{request_id:string}`，限制为 16–80 个 ASCII 字母、数字、`-`、`_`。同一 ID 同一任务返回已持久化记录；同一 ID 不同任务 409。幂等窗口为历史保留期；删除过期记录才释放 ID，UI 不复用已完成操作的 ID。
- 分页 `?before=<runID>&limit=20`，limit 1–100，按创建顺序倒序，响应 `{items:Run[], next_before:string|null}`；非法 cursor 返回 400，不扫描任意文件路径。

### Go 类型（均在注入后的 core module 内）

`internal/scheduler/console.go` 定义；字段按 snake_case 添加 JSON tags：

```go
type TaskInfo struct {
    ID string
    Enabled bool
    Hours []int
    Timezone string
    NextAt *time.Time
}
type Balance struct { Value int64; ObservedAt time.Time }
type AccountResult struct {
    UID string
    Status string
    Detail string
    Before *Balance
    After *Balance
    Reward *int64
}
type TaskResult struct {
    Accounts []AccountResult
    Log string
    LogTruncated bool
}
func (s *Scheduler) TaskCatalog(now time.Time) []TaskInfo
func (s *Scheduler) ExecuteTask(ctx context.Context, taskID string) (TaskResult, error)
```

只在既有 `scheduler.Config` 添加可选 `Scheduled func(context.Context, string, time.Time)`；Run 将计划时点传入该接点，nil 保持原 dispatch 行为。`ExecuteTask` 直接调用同一实例已有方法，不再调用 Scheduled，避免递归。现有 CLI 入口仍可用。

`internal/taskrun/store.go` 定义；JSON 字段一律 snake_case，未知指针值显式 null：

```go
type Run struct {
    ID string
    TaskID string
    Source string // manual 或 scheduled
    RequestID string
    ScheduledAt []time.Time
    StartedAt time.Time
    FinishedAt *time.Time
    DurationMS *int64
    Status string
    Accounts []scheduler.AccountResult
    Log string
    LogTruncated bool
}
type Store struct { mu sync.Mutex; path string; runs []Run }
func OpenStore(path string, now time.Time) (*Store, error)
func (s *Store) Put(run Run, now time.Time) error
func (s *Store) Get(id string) (Run, bool)
func (s *Store) ByRequest(id string) (Run, bool)
func (s *Store) Page(before string, limit int) ([]Run, string, error)
```

`Put` 是唯一写入入口，先写临时文件、Sync、Close、Rename、目录 Sync，再发布内存副本；保存失败不改变内存真相。历史 JSON 外层 `{version:1,runs:[]}`。读取返回深拷贝，不泄露可变 slice。

```go
type Runner struct {
    mu sync.Mutex
    ctx context.Context
    store *Store
    catalog func(time.Time) []scheduler.TaskInfo
    execute func(context.Context, string) (scheduler.TaskResult, error)
    active *Run
    done chan struct{}
}
func NewRunner(ctx context.Context, store *Store,
    catalog func(time.Time) []scheduler.TaskInfo,
    execute func(context.Context, string) (scheduler.TaskResult, error)) *Runner
func (r *Runner) StartManual(taskID, requestID string) (Run, error)
func (r *Runner) Scheduled(ctx context.Context, taskID string, at time.Time)
func (r *Runner) Active() *Run
type BusyError struct { RunID string }
func (e *BusyError) Error() string
var ErrDisabled = errors.New("task disabled")
var ErrUnknownTask = errors.New("unknown task")
var ErrRequestConflict = errors.New("request id belongs to another task")
```

bridge 构造契约：

```go
type Config struct {
    Key string
    APIKey string
    AuthDir string
    UpstreamCommit string
    PatchIdentity string
    GlobalEnabled bool
    Pool *pool.Pool
    Upstream *upstream.Client
    Scheduler *scheduler.Scheduler
    Tasks *taskrun.Runner
    History *taskrun.Store
    TaskError error
    Public http.Handler
}
func New(ctx context.Context, cfg Config) http.Handler
```

Task 2 先使用不含 Tasks/History/TaskError 的 Config；Task 6 原位增加三个字段。不定义空 taskrun 包占位；其余字段签名不变。TaskError 保存启动时历史故障，相关管理端点 503，不能阻止 Public 启动。

console 独立 module 不 import core internal 包，只使用 JSON 协议：

```go
type Config struct {
    CoreURL *url.URL
    AdminKey string
    APIKey string // 仅已鉴权 /admin/access 展示现有兼容信息
    BridgeKey string
    PublicOrigin string
}
func NewServer(cfg Config) (http.Handler, error)
```

## Task 1：保全当前实现，建立纯净快照与可复现物化

**Files:** 新增 `scripts/overlay.py`、`scripts/test_overlay.py`、`upstream/`、`upstream.lock`、`patches/series`、`patches/README.md`；修改 `.gitignore`。原始源码此时不删除。

**Interfaces:**
- Consumes: Git commit、现有 dirty worktree；Python `pathlib.Path`。
- Produces: `source_digest(source: Path) -> str`、`export_snapshot(repo: Path, commit: str, dest: Path) -> None`、`materialize(root: Path, dest: Path) -> None`；CLI `python3 scripts/overlay.py prepare --output .build/core`，输出目录必须不存在。

- [ ] 记录 `git status --short`、`git diff --stat`、`git log -5 --oneline`、`git remote -v`。用 `mktemp -d` 在仓库外生成私有备份目录；保存 Git bundle、binary diff，以及当前 cmd/internal/scripts/文档/部署文件、未跟踪 OAuth/UI/test 源码。备份包括忽略的相关源码，排除真实 auths/data/.env；用户 `.agents/` 留在原位，不清理。
- [ ] 写摘要和不可覆盖测试；测试采用临时合成文件，不依赖工作目录用户数据：

```python
def test_digest_tracks_content_and_executable_mode(self):
    with tempfile.TemporaryDirectory() as d:
        root = Path(d)
        path = root / "run.sh"
        path.write_bytes(b"exit 0\n")
        path.chmod(0o644)
        first = source_digest(root)
        path.chmod(0o755)
        self.assertNotEqual(first, source_digest(root))
        path.write_bytes(b"exit 1\n")
        self.assertNotEqual(first, source_digest(root))
```

- [ ] 运行 `python3 -m unittest discover -s scripts -p test_overlay.py -v`，先确认缺少实现导致失败，不是环境/导入路径问题。
- [ ] 实现摘要：按 POSIX 相对路径排序，每项编码路径、Git 模式、内容 SHA-256，使用确定性 JSON 编码后 SHA-256。拒绝目录外 symlink、嵌套 `.git`、设备文件；普通文件模式规范化 100644/100755，symlink 120000 摘要取链接文字而非目标内容。

```python
record = [relative_path, git_mode, hashlib.sha256(content).hexdigest()]
payload = json.dumps(records, ensure_ascii=False, separators=(",", ":")).encode()
return hashlib.sha256(payload).hexdigest()
```

- [ ] 从本地 Git 对象的固定 SHA 导出快照，不从当前 dirty 文件反推基线。读取 `git ls-tree -r -z` 和 `git cat-file blob` 保留所有跟踪源码/模式，拒绝 gitlink；写实际摘要到 lock。`upstream/` 不复制 `.git`。
- [ ] 实现 materialize：校验 lock → 复制到新目录 → 校验 extensions 每一文件无同名/越界，目标父路径不能经过 symlink → 按 series 顺序执行 `git apply --check` 和 `git apply`。series 文件名拒绝绝对路径/`..`/重复项；工具每条 subprocess 用参数数组、`check=True`，不 shell 拼接；所有失败返回非零。

```python
subprocess.run(["git", "apply", "--check", str(patch)], cwd=dest, check=True)
subprocess.run(["git", "apply", str(patch)], cwd=dest, check=True)
```

- [ ] 增加合成 Git fixture 的冲突、重复补丁、扩展重名、路径穿越、损坏 lock、目标已存在测试；物化前后 source_digest 相同。失败只清理由本次创建并验证的临时目录，不碰现有 `.build/core`。
- [ ] 收窄根 ignore 的 Markdown/docs 规则以跟踪定制文档和上游原文档，仍忽略 `.agents/`、凭据、备份、`.build/`；用 `git check-ignore` 验证样例，不通过 `git add .` 拉入用户技能。
- [ ] GREEN：上述 unittest 全通过；两次不同新目录物化的源码摘要一致。记录备份位置与恢复方式，提交本任务明确路径，commit：`build: pin pristine upstream and verify overlays`。

## Task 2：迁移凭据安全修复和 core 授权桥接

**Files:** `extensions/internal/oauth/`、`extensions/internal/pool/install*`、`extensions/internal/upstream/reauthorize_test.go`、`extensions/internal/bridge/{bridge,oauth}{,_test}.go`、`extensions/cmd/server/extension{,_test}.go`；补丁 0001/0002/0003/0005 与 README。

**Interfaces:**
- Consumes: Task 1 materialize；现有 `oauth.New(realm string) (*oauth.Client,error)`、`Start(context.Context) (string,string,error)`、`Poll(context.Context,string) (*auth.Auth,error)`、`(*pool.Pool).Install(*auth.Auth) error`。
- Produces: 跨任务契约的 `bridge.New`，不含任务字段；core `/internal/v1/info/status/models/chat/oauth` 与 owner 取消端点；main 最小 wrapper `wrapCore(ctx context.Context, cfg *Config, p *pool.Pool, up *upstream.Client, sch *scheduler.Scheduler, public http.Handler) (http.Handler,error)`。

- [ ] 将当前 OAuth、install、reauthorize 新文件按地图复制到 extensions。当前 auth/pool/upstream diff 逐段分类，新增文件不进入 patch，旧文件安全改动拆进 0001/0002。审查所有 snapshot/save/refresh/header/选号/出站调用者，不漏掉 fallback 路径。
- [ ] 在 bridge 测试新增错误 key 的最小 RED 用例（同 package bridge，imports 使用标准库和原包）：

```go
func TestBridgeRejectsWrongKey(t *testing.T) {
    h := New(context.Background(), Config{Key: strings.Repeat("b", 32)})
    req := httptest.NewRequest("GET", "/internal/v1/info", nil)
    req.Header.Set("Authorization", "Bearer wrong")
    w := httptest.NewRecorder()
    h.ServeHTTP(w, req)
    if w.Code != http.StatusUnauthorized { t.Fatalf("status=%d", w.Code) }
}
```

- [ ] 物化到新 `.build/task2-red`，运行 `go -C .build/task2-red test ./internal/bridge ./internal/oauth ./internal/pool ./internal/upstream`，确认新增桥接测试 RED。
- [ ] 从原 `internal/server/admin.go` 移动 loginFlow/startLogin/pollLogin/finishLogin/completeRegion 的 core 部分到 bridge/oauth.go；保留每流程 cookie jar、10 分钟过期、8 个并发流程上限、UID/realm/可信 HTTPS URL/响应大小限制。owner 使用独立随机会话身份，取消接口仅作用于该 owner。
- [ ] bridge 用 `http.ServeMux` 精确路由，先密钥校验再读请求体，拒绝未列出的路径；读取 OAuth 与地区请求沿用 8192 字节/未知字段拒绝。输出仅沿用 `loginFlow.respond` 的安全字段，绝不序列化 auth.Auth。

```go
mux.HandleFunc("GET /internal/v1/info", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{
        "protocol": 1, "upstream_commit": cfg.UpstreamCommit,
        "patch_identity": cfg.PatchIdentity, "global_enabled": cfg.GlobalEnabled,
    })
})
```

- [ ] main 仅在现有 cfg/p/up/sch/context 建立后调用 wrapCore；bridge 内部 models/chat 转给原 public Handler 并注入 core 的 API key，外部 public 不改变。新增 liveness 放 wrapper；未配置桥接模式保持原 CLI/服务使用方式。
- [ ] 迁移 CLI 共用 OAuth 的必要 patch；保留 11217 pending、未知错误失败、保存后发布、live Auth 身份、旧刷新拒绝、重授权不清 unrelated limits、全局账号地区选择/激活持久化与所有路由拦截的现有测试。补 bridge 跨 owner GET/region/取消的 404 测试；stub OAuth 不联网。
- [ ] GREEN：重新物化到 `.build/task2-green`，运行上述测试及 `go -C .build/task2-green test -race ./internal/auth ./internal/pool ./internal/upstream ./internal/bridge`。只提交本任务扩展和补丁，commit：`refactor: isolate core authorization extensions`。

## Task 3：抽出独立 console，保留公共 API 与流式取消

**Files:** `console/go.mod`、`main.go`、`server.go`、`proxy.go`、对应 `_test.go`、`web/`、`web_test.cjs`、`browser_preview_test.go`。

**Interfaces:**
- Consumes: Task 2 HTTP v1 与 `X-Console-Owner`；跨任务 Config。
- Produces: `NewServer(cfg Config) (http.Handler,error)`，原 `/admin/login/session/logout/status/models/chat/access/oauth` 与静态页面；公共 `/v1/*`、`/status`、`/healthz`、console `/livez`。

- [ ] 先移动现有原生页面、SSE parser 测试和浏览器夹具，不改样式与既有行为；Go module 单独声明 `module workbuddy2api-console`，使用 Go 1.22.5 最低语言版本，不 import upstream internal。
- [ ] 新增公共代理无凭据注入测试：

```go
func TestPublicProxyDoesNotBorrowCredentials(t *testing.T) {
    core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if got := r.Header.Get("Authorization"); got != "" { t.Errorf("unexpected auth") }
        w.WriteHeader(http.StatusUnauthorized)
    }))
    defer core.Close()
    target, _ := url.Parse(core.URL)
    h, err := NewServer(Config{CoreURL: target, AdminKey: strings.Repeat("a",32),
        APIKey: strings.Repeat("p",32), BridgeKey: strings.Repeat("b",32)})
    if err != nil { t.Fatal(err) }
    w := httptest.NewRecorder()
    h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/models", nil))
    if w.Code != 401 { t.Fatalf("status=%d", w.Code) }
}
```

- [ ] RED：`go -C console test ./...`；然后提取会话/登录限流/CSRF/origin/安全响应头到 server.go。继续 8 小时 HttpOnly/SameSite=Strict cookie，HTTPS origin 时 Secure，页面不存浏览器 localStorage/sessionStorage 密钥。
- [ ] proxy.go 使用标准库 `httputil.ReverseProxy`，公共与管理两个明确入口，固定 CoreURL，`FlushInterval: -1`。公共转发剥除 bridge/owner/转发头但保留客户端 API Bearer；管理代理在校验 session+CSRF 后注入桥接 key 和服务端 owner。

```go
proxy := &httputil.ReverseProxy{
    Rewrite: func(pr *httputil.ProxyRequest) {
        pr.SetURL(cfg.CoreURL)
        pr.Out.Header.Del("X-Console-Owner")
        pr.Out.Header.Del("X-Forwarded-For")
        pr.Out.Header.Del("X-Forwarded-Host")
        pr.Out.Header.Del("X-Forwarded-Proto")
    },
    FlushInterval: -1,
}
```

- [ ] 管理协议探测失败/不等于 1 阻止相关管理动作，返回明确 503；公共模型 API 不受影响。配置固定 URL 禁止 userinfo/query/fragment/非 HTTP(S)，PUBLIC_ORIGIN 沿用严格校验；不信任入站 X-Forwarded 判断安全 origin。
- [ ] 迁移会话/CSRF/限流/跨会话/退出取消测试；新增恶意 path 编码不能命中 internal、管理未登录401、跨源403、core不可达503、协议不匹配503、Bearer原样保留。SSE mock 用 Flush 发送首帧并等待 `r.Context().Done()`，验证客户端断连向 core 传播，不能只测最终完整字符串。
- [ ] GREEN：`go -C console test -race ./...`、`node --test console/web_test.cjs`；浏览器 mock 验证原四页与停止按钮。提交 console 明确文件，commit：`refactor: extract standalone web console`。

## Task 4：双容器启动和旧密钥/数据迁移的第一阶段验收

**Files:** `deploy/core.Dockerfile`、`deploy/console.Dockerfile`、`deploy/migrate.py`、`deploy/test_migrate.py`、`deploy/compose.acceptance.yml`、`docker-compose.yml`、`.dockerignore`、`extensions/cmd/server/extension.go`/测试、`scripts/acceptance.sh`、README。

**Interfaces:**
- Consumes: Task 1 prepare、Task 2 core wrapper、Task 3 console binary；现有 `/app/data/console-keys.json` 的 `admin_key/api_key`。
- Produces: `initializeKeys(dataDir, keyDir, adminOverride, apiOverride string) (deploymentKeys,error)`，`deploymentKeys` 只含 `AdminKey/APIKey/BridgeKey string`；凭据卷 `/run/wb2a/keys.json`。CLI `python3 deploy/migrate.py inspect --container NAME` 输出脱敏 JSON 迁移清单，`backup --manifest FILE --output DIR` 备份已解析明确卷，不切换服务。

- [ ] 先写 initializer 测试：临时 data 预写有效旧 key 文件；调用两次并比较 admin/API 不变、bridge 长度至少32且独立；目标损坏/目标已有不同 key 都失败，旧文件字节不变。

```go
old := []byte(`{"admin_key":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","api_key":"pppppppppppppppppppppppppppppppp"}`)
if err := os.WriteFile(filepath.Join(dataDir,"console-keys.json"), old,0600); err != nil { t.Fatal(err) }
first, err := initializeKeys(dataDir,keyDir,"","")
if err != nil { t.Fatal(err) }
second, err := initializeKeys(dataDir,keyDir,"","")
if err != nil || first != second { t.Fatalf("non-idempotent initialization: %v",err) }
```

- [ ] RED：新目录物化后运行 `go -C .build/task4-red test ./cmd/server -run 'Test.*Keys' -v`；实现沿用当前 bootstrap 的原子落盘，新增 key 文件 0600/目录0700 与父目录同步；损坏不轮换。配置覆盖必须通过长度/三钥不同校验，不能偷偷改变旧持久 key；两个服务读取同一生效布局。
- [ ] core 是唯一 key 初始化者；console 以有界等待读取，失败退出由 restart 策略重试。core 不日志打印 key；console 只按现有约定输出生成的管理 key；桥接/API 不写日志。旧文件保留供回退，迁移冲突明确失败。
- [ ] core Dockerfile 复制干净快照/overlay 脚本/扩展/补丁，在构建层 prepare 并运行 Go 测试再编译原各 CLI，保留 Python/bash/CA/tzdata。console 单独编译并内嵌页面；非 root UID10001，各自 livez；空池 healthz 非就绪但 livez 正常。
- [ ] Compose 使用两个服务、仅 console ports、core healthcheck依赖、key 卷 console只读：

```yaml
services:
  console:
    build:
      context: .
      dockerfile: deploy/console.Dockerfile
    ports: ["${WB2A_PORT:-7863}:7863"]
    volumes: ["keys:/run/wb2a:ro"]
    depends_on:
      core:
        condition: service_healthy
  core:
    build:
      context: .
      dockerfile: deploy/core.Dockerfile
    volumes: ["auths:/app/auths", "data:/app/data", "keys:/run/wb2a"]
```

- [ ] 补齐 restart、TZ、监听/路径与 PUBLIC_ORIGIN 环境配置、healthchecks、卷声明；auths/data 卷名支持明确复用原卷。生产与验收项目名/端口分离；console 不挂 auths/data；不使用 `internal: true` 网络阻断 core 正常访问上游。
- [ ] migrate.py 用 `docker inspect` 解析 Mounts，仅输出 Name/Source/Destination/Type 和容器身份，不输出 Config.Env。命名卷和 bind 都支持，未知/缺失映射失败。backup 对已确认映射创建受限归档，记录散列/大小，测试写入临时夹具而非真实令牌。活跃运行的备份标注不构成最终一致性备份，切换前停旧实例后再做最终备份。
- [ ] GREEN：initializer + `python3 -m unittest discover -s deploy -p test_migrate.py -v`；`docker compose config --quiet`；构建两镜像；独立模拟卷验证新启动、原 key/账号/状态重建保留和单入口。第一阶段未通过不得开始 Task 5；不切换当前真实容器。提交 commit：`feat: compose isolated core and console with safe data migration`。

## Task 5：给原 scheduler 增加目录与可验证结果观察点

**Files:** `extensions/internal/scheduler/console.go`、`console_test.go`、`extensions/scripts/task_events.py`、`test_task_events.py`、0004 patch。

**Interfaces:**
- Consumes: 原 `Scheduler.cfg/nextFire/dispatch`、`CheckinAll() ([]CheckinOutcome,error)`、其他五个 `Run*Now()`、`Pool.List/AuthByUID` 与 Auth Snapshot。
- Produces: 跨任务 `TaskInfo/Balance/AccountResult/TaskResult`、`TaskCatalog/ExecuteTask`、Config.Scheduled。脚本结构化事件 stdout 前缀 `WB2A_TASK_EVENT `，JSON 仅允许 `uid/status/detail/reward`，其中 reward 可 null；普通输出仅脱敏摘要。

- [ ] 测试目录使用同一时区计算下个时点且禁用返回 null；直接使用既有 New 默认值，不复制 nextFire：

```go
func TestTaskCatalogUsesSchedulerConfig(t *testing.T) {
    zone := time.FixedZone("Asia/Shanghai",8*3600)
    s := New(Config{CheckinHours: []int{11}, TravelDisabled:true})
    list := s.TaskCatalog(time.Date(2026,9,15,10,0,0,0,zone))
    if len(list)!=6 { t.Fatalf("tasks=%d",len(list)) }
    for _, v := range list {
        if v.ID=="checkin" && (v.NextAt==nil || v.NextAt.Hour()!=11) { t.Fatal(v) }
        if v.ID=="travel" && (v.Enabled || v.NextAt!=nil) { t.Fatal(v) }
    }
}
```

- [ ] RED：物化后 `go -C .build/task5-red test ./internal/scheduler`。新增目录方法从 s.cfg 读取六项配置，统一转北京时间，再调用原 nextFire；main 仍使用 Normalize 后的真实配置。
- [ ] patch Run 内原 `s.dispatch(k)` 的接点，保持 nextWake、timer、同点顺序不变：

```go
if s.cfg.Scheduled != nil {
    s.cfg.Scheduled(ctx, s.taskID(k), next)
} else {
    s.dispatch(k)
}
```

`func (s *Scheduler) taskID(k taskKind) string` 在 console.go 用六项 switch 返回白名单 ID；未知返回空，调用方拒绝。Task 6 接入 callback 前保持 nil。

- [ ] ExecuteTask 以白名单 switch 调用已有逻辑；checkin 调用 CheckinAll 收集结构化状态，其余为当前同步调用安装短期观察器。观察器绑定该 scheduler 当前执行上下文并 mutex 保护，不截获全进程 log.Default 或聊天日志。
- [ ] 为 travel/activity/keepalive 每个账号的成功、失败、跳过分支增加观察调用；原 limit/delay/window/claimed/realm 判定不移动。观察器方法契约 `func (s *Scheduler) observe(result AccountResult)`，无观察器为 no-op；多步骤单账号结果合并不能覆盖先前失败。只有上游明确奖励值进入 Reward，原余额进入 After 并记录时刻。
- [ ] 脚本执行前从同一个 pool 选符合条件且非 pending/disabled 的 CN 账号，读取一致 Auth 快照，写入私有临时目录供 `WB2A_AUTHS` 使用；脚本不读取 live auths。既有 Python 通过 task_common 只读 token，不允许把临时旧凭据覆盖回 live pool；如上游更新引入凭据写回，适配校验失败而非自动导入。无合格账号不拉子进程。
- [ ] 保留固定命令与 ALL 参数，通过 cmd.Env 指向筛选快照；child stdout/stderr 接入有界脱敏 writer，`exec.CommandContext` 用 core 生命周期，而非管理请求 context。凭据不放命令行，结束销毁本次确切临时目录；读取快照不长时间锁住聊天。

```python
def emit(uid, status, detail, reward=None):
    if status not in {"success", "failed", "skipped", "unknown"}:
        raise ValueError("invalid task status")
    print("WB2A_TASK_EVENT " + json.dumps({"uid": uid, "status": status,
          "detail": detail, "reward": reward}, ensure_ascii=False), flush=True)
```

- [ ] task_events.py 经新增文件注入，0004 在 Python 现有实际结果分支调用 emit，保留算法和输出。Detail 只用适配器固定分类文字，不拼原 HTTP body；普通输出清洗后摘要保存，不确定内容丢弃为“上游输出已省略”。解析有界，非法 JSON/无结构化事件/exit0未知均不能转 success；明确非零退出 failed。处理历史 Python 脚本自己的测试兼容。
- [ ] GREEN：Go scheduler 全测试/竞态和 Python unittest，mock 覆盖窗口外、活动关闭、已领取、无账号、global、pending、单账号失败不阻断、真实 reward 与余额不同、未识别输出和非零退出；不运行真实活动脚本。提交 commit：`feat: expose scheduler outcomes without duplicating task logic`。

## Task 6：持久化任务记录与唯一执行入口

**Files:** `extensions/internal/taskrun/{store,runner}{,_test}.go`；bridge.Config 与 extension.go 接线。

**Interfaces:**
- Consumes: Task 5 `TaskCatalog/ExecuteTask`、core lifecycle context。
- Produces: 跨任务 Store/Run/Runner 全部接口；由 core 唯一实例持有，bridge 只调用同一 Runner。

- [ ] 写 Store 的重启测试，运行中恢复 interrupted 且不执行任何 callback：

```go
func TestRestartMarksRunningInterrupted(t *testing.T) {
    path := filepath.Join(t.TempDir(),"runs.json")
    now := time.Now().UTC()
    s,err := OpenStore(path,now)
    if err!=nil { t.Fatal(err) }
    err=s.Put(Run{ID:"run-1",TaskID:"checkin",Source:"manual",RequestID:"request-1234567890",
        StartedAt:now,Status:"running"},now)
    if err!=nil { t.Fatal(err) }
    reopened,err := OpenStore(path,now.Add(time.Minute))
    if err!=nil { t.Fatal(err) }
    got,ok := reopened.Get("run-1")
    if !ok || got.Status!="interrupted" { t.Fatal(got) }
}
```

- [ ] RED：新物化目录 `go test ./internal/taskrun`；实现 version1 JSON Store，加载严格校验 ID唯一/状态/时间/数组大小，损坏不重写。恢复记录耗时只标观测中断时刻，不宣称已知业务结束；保留未知结果说明。
- [ ] Put 内持有 mutex 构造深拷贝新记录集，30天/1000条仅清理终态；日志 UTF-8 安全截断至65536字节。写、sync、rename成功才赋值 s.runs；测试故障可用私有写函数注入 `func(path string, data []byte) error`，不依赖 chmod 在 root 下失效。
- [ ] 写 Runner 并发/幂等测试，execute 用 channel 阻塞，不 sleep 等竞态：

```go
entered, release := make(chan struct{}), make(chan struct{})
execute := func(ctx context.Context,id string)(scheduler.TaskResult,error){
    close(entered)
    select { case <-release: case <-ctx.Done(): }
    return scheduler.TaskResult{},nil
}
catalog := func(time.Time)[]scheduler.TaskInfo{
    return []scheduler.TaskInfo{{ID:"checkin",Enabled:true}}
}
runner := NewRunner(context.Background(),store,catalog,execute)
first,err := runner.StartManual("checkin","request-1234567890")
if err!=nil { t.Fatal(err) }
<-entered
same,err := runner.StartManual("checkin","request-1234567890")
if err!=nil || same.ID!=first.ID { t.Fatal("retry started a second run") }
close(release)
```

该测试先在 `t.TempDir()` 用 OpenStore 建立 store；t.Cleanup 始终取消 lifecycle，防止失败时遗留 goroutine。

- [ ] StartManual 在一个临界区先查已保留 requestID，再检查白名单/启用/忙碌，写开始记录成功才启动 goroutine。唯一 active 使用 `done chan struct{}` 通知等待者；保留注释 `ponytail: background tasks are globally serial; use per-account scheduling only if throughput requires it`。
- [ ] Scheduled 持锁发现“同任务、来源manual、仍active”时把计划时点去重写入当前记录，然后等该 done；合并写失败只告警缺失的触发记录，不重跑已执行中的任务。不同任务等 done 后重查并执行，保持 scheduler 顺序。请求相同 ID 的重试先于 busy 判断；任务不同的 ID 重用返回 ErrRequestConflict。
- [ ] 结束记录从最新 active 获取，避免覆盖已合并 ScheduledAt；无账号→skipped，全部skip→skipped，任意unknown且无已知失败→unknown，全部成功或成功+skip→success，全部失败（允许skip）→failed，失败与success/unknown混合→partial_failure。保留每账号细节；context取消→interrupted；panic受控记录unknown并释放槽位，不复跑。
- [ ] 开始写失败不调用 execute；自动跳过并固定分类告警。完成写失败保留磁盘running、日志告警且释放内存执行锁；同requestID仍返回旧记录不可再执行业务。存储错误通过 bridge 503 显示。core退出取消执行，不补跑。
- [ ] main 在 scheduler.New 前设置 Config.Scheduled 闭包，闭包调用启动前构造好的 Runner；New 后传入 sch.TaskCatalog/sch.ExecuteTask。必须在 `go sch.Run(ctx)` 前完成 Runner 赋值；OpenStore失败配置只告警跳过计划并使任务管理503，Public照常启动，不退回无记录调度。
- [ ] GREEN：`go test -race ./internal/taskrun ./internal/scheduler ./internal/bridge ./cmd/server`，覆盖同任务合并/不同任务等待、计划同点顺序、完成写失败、损坏历史、公用 API 不受影响、保留期/数量/日志/null余额。提交 commit：`feat: persist and serialize scheduled and manual runs`。

## Task 7：接通任务 API 与「自动任务」页面

**Files:** `extensions/internal/bridge/bridge.go`/测试；`console/server.go`/测试、`console/web/{index.html,app.js,style.css}`、`console/web_test.cjs`、browser_preview_test.go。

**Interfaces:**
- Consumes: Runner/Store、HTTP v1、TaskInfo/Run JSON。
- Produces: Spec 四个 `/admin/tasks*` 路由；`GET tasks` 返回 `{items:TaskInfo[],active_run:Run|null,latest_runs:Run[]}`，其中 latest_runs 每任务最多1条。trigger 202 返回 Run，busy409含run_id；分页遵循跨任务契约。

- [ ] 新增 HTTP 测试：未登录401、缺CSRF403、未知任务404、body/ID非法400、禁用409、运行中409、存储不可写503，mock 计数确认均未执行；同 request 重试202同runID且总执行次数1。
- [ ] 浏览器 Node 测试先定义展示未知值逻辑的 RED 用例；迁移现有 VM fake DOM 测试工具，不新增 jsdom：

```js
assert.equal(formatTaskReward(null), '未确认');
assert.equal(formatTaskReward(0), '0');
assert.equal(formatTaskReward(5), '5');
```

在 app.js 新增纯函数 `function formatTaskReward(value)`，先严格判断 `value === null || value === undefined`，不要用 `value || 0`。Go emit 的字段和 UI 读取字段逐一核对。

- [ ] RED：`go -C console test ./...`、core bridge定向测试、`node --test console/web_test.cjs`。接入准确路由，分页限制1–100，请求体8192字节，任务ID白名单；不接受任意脚本/账号路径/URL参数。
- [ ] 原导航新增「自动任务」，复用当前卡片样式，包含六项配置、北京时间、next_at、active/latest、立即执行；没有next_at显示“已禁用”，不在 JS 推导 schedule。
- [ ] 发起操作时生成一次 `crypto.randomUUID()`，网络重试沿用该 ID；明确开始新操作才换 ID。请求等待与任意active时禁用按钮，禁用任务永久按配置禁用；不乐观显示成功、不因页面超时自动创建新ID。

```js
const response = await api(`/admin/tasks/${taskID}/runs`, {
  method: 'POST', body: JSON.stringify({request_id: requestID})
});
```

复用既有 api helper 的 cookie/CSRF/error 行为。关闭/切页停止轮询并取消读取请求，不请求取消已接受任务；查看详情展示逐账号状态、明确奖励、带时间余额和脱敏日志，日志使用 textContent。

- [ ] 列表轮询仅当前任务页可见时运行，完成后刷新状态；记录游标分页、错误重试、busy跳转当前运行；访问性保留 label、键盘操作、aria-live，手机侧栏/退出按钮可用。
- [ ] GREEN：Go/Node全部通过；真实浏览器对 mock core 验证列表、立即执行、快速双击、断网重试、部分失败、unknown、日志截断、分页、页面重载后记录、窄屏。报告这些是 mock 结果，不是真实奖励。提交 commit：`feat: add automatic tasks page and execution history`。

## Task 8：可失败回退的手动上游更新入口

**Files:** `scripts/overlay.py`、`test_overlay.py`、`check.sh`、`acceptance.sh`、patches/README.md、README。

**Interfaces:**
- Consumes: prepare、镜像构建、模拟验收。
- Produces: `update(root: Path, ref: str) -> None`；CLI `python3 scripts/overlay.py update --ref REF`，必须显式ref，默认仓库从 lock读取但仅允许批准的 canonical URL；成功只更新 upstream/lock 工作文件。

- [ ] 用 unittest.mock 注入候选验证失败，验证旧 lock/快照摘要不变且部署命令未执行：测试给 subprocess.run 的 side_effect 记录参数，断言无真实项目的 compose up/restart、无 git commit/push。
- [ ] RED：`python3 -m unittest discover -s scripts -p test_overlay.py -v`；实现预检 `git status --porcelain -- upstream upstream.lock extensions patches deploy scripts console docker-compose.yml`，这些路径存在改动就拒绝覆盖，文档等无关改动不阻断。
- [ ] ref 参数禁止以 `-` 开头/控制字符；在 mktemp 候选 Git 目录从 canonical源获取指定ref，解析 `FETCH_HEAD^{commit}` 完整SHA，不以远程默认分支替代失败ref。导出 candidate/upstream 并写实际lock，复制当前扩展/补丁/部署/console到candidate。
- [ ] 候选验证入口为 `bash scripts/check.sh [repository-root]`，参数缺省时从脚本所在目录定位根，内部创建全新 prepared 目录；`bash scripts/acceptance.sh [repository-root]` 使用相同根参数契约。update 调用候选自己的两个入口并传入候选绝对路径。check.sh 用确切新目录 prepare，执行以下命令；shell `set -eu`，不使用 `|| true`：

```sh
go -C "$prepared" test ./...
go -C "$prepared" vet ./...
go -C "$prepared" test -race ./internal/auth ./internal/pool ./internal/upstream ./internal/scheduler ./internal/taskrun ./internal/bridge
go -C "$candidate/console" test -race ./...
node --test "$candidate/console/web_test.cjs"
python3 -m unittest discover -s "$prepared/scripts" -p 'test_*.py'
```

`prepared` 和 `candidate` 是脚本解析得到的绝对临时路径，不是用户传来的 shell 片段。镜像构建与 acceptance.sh 使用独立候选项目/随机可用端口/模拟卷，任务网络指向 mock，不复用当前真实卷或 origin设置。仅验收 Compose 网络设为 `internal: true` 并包含 mock 服务，从网络层阻断向真实上游回退；镜像依赖在进入隔离运行前构建完成。
- [ ] 所有候选验证通过后，再次检查相关路径干净且lock与起始相同；在同文件系统保留旧快照/锁文件备份后安装新组合。写迁移journal记录阶段，发生文件替换异常恢复旧组合；下次启动检测未完成journal明确恢复/失败，不继续构建半更新状态。相关路径变动时中止，不覆盖并发用户编辑。
- [ ] 增加测试：拉取失败、清单错误、冲突/重复patch、扩展碰撞、Go失败、镜像失败、验收失败、发布文件阶段失败、二次dirty检查失败均保留旧组合；成功只变快照/lock，无自动commit/deploy。记录候选路径方便诊断，不自动清用户数据。
- [ ] GREEN：unittest和本地合成小repo升级fixture；不为测试主动升级真实上游版本。README给出查看diff→提交→明确部署→按旧组合回退的命令顺序与保留卷说明。提交 commit：`build: validate upstream candidates before replacing snapshots`。

## Task 9：清理旧布局、总体验收与安全切换交付

**Files:** 根旧源码路径的受控移除、最终 README、`.gitignore`、`.dockerignore`、`docs/superpowers/verification/2026-09-15-overlay-migration.md`；只对映射账本中确认路径执行。

**Interfaces:**
- Consumes: Task 1 备份与迁移账本、Task 4 inspect/backup、Task 8 check/acceptance；现有容器实际挂载。
- Produces: 单一源码维护布局、可审阅补丁清单、启动/更新/回退文档、测试与迁移证据。实际切换仅在无在途任务/请求且备份验证后；无法确认安全就停在已验证待切换并向用户说明。

- [ ] 对比开始时 dirty/new 清单与现在每个扩展/patch；原 `internal/server/handler.go` 只保留仍必要patch，其admin注册应已迁出；原bootstrap存储逻辑已被迁移；handler_test ledger修正明确记录原因。没有承接位置的改动先查清，不能默默删掉。
- [ ] 执行删除旧根源码的前置断言脚本：每个原跟踪文件在基线 manifest 中有 upstream 对应，所有自定义diff在账本有承接，备份归档存在且可读；用 `git diff --no-index` 对比物化源码与备份中的关键实现，业务差异逐项说明。只移除已验证确切文件，用户 `.agents/`、.env、auths/data 保持原样。
- [ ] 删除旧根 `Dockerfile`（新默认Compose明确使用deploy路径）、Go模块及重复cmd/internal；原业务scripts移除根副本但保留新的overlay/check/acceptance。根config.example等由README指向upstream版本，需要适配的部署样例放deploy。用apply_patch/精确git rm，不递归删除广目录，不 reinit Git。
- [ ] 总体验收执行顺序：

```sh
python3 -m unittest discover -s scripts -p test_overlay.py -v
python3 -m unittest discover -s deploy -p test_migrate.py -v
bash scripts/check.sh
docker compose config --quiet
docker compose build
bash scripts/acceptance.sh
git diff --check
```

- [ ] acceptance 同时验证第一阶段和任务阶段：非root/单端口/空账号存活、错误key/CSRF/owner/协议、OAuth11217与激活/重授权、SSE首帧和取消、六任务资格与unknown语义、并发/幂等/计划合并、历史坏文件不影响聊天、模拟数据重启保留。测试不会读取本机已登录账号；core mock目标封闭，无法意外退回生产默认域名。
- [ ] 使用实际浏览器完成mock任务页和原四页桌面/窄屏检查，存截图和脱敏验证摘要。验证前后 upstream摘要一致；镜像源SHA/patch_identity与info一致；README明确第一次管理key领取和人工登录步骤。
- [ ] 真正切换前只读 inspect 当前容器并展示计划复用卷；原已知名称只是线索，必须现场解析。检查活跃请求/任务，旧版无可观测能力时请求用户暂停使用并确认维护窗口，不能因一次无日志判定空闲。
- [ ] 安全窗口内停止已确认旧实例（不删卷），执行最终一致性备份并验证归档，配置已解析实际 auths/data卷名及原宿主端口，再启动新Compose。遇到挂载/迁移/key冲突停止，不生成替代空数据继续；回退复用旧镜像/服务定义/原卷，保留新key/task记录供诊断。
- [ ] 真环境仅检查启动、旧key可进入、已有账号状态、保存数据一致；真实登录、模型消费或任务奖励测试请用户操作，不自动点“立即执行”。若安全切换尚需用户窗口，不把构建/mock通过说成已上线。
- [ ] 最后只提交本任务准确路径，commit：`refactor: complete overlay layout and document operations`；交付锁定SHA/patch清单、启动更新方式、测试范围、备份位置、卷映射、切换状态和已知限制。不推送原origin、不默认发布registry。

## 计划自检与执行边界

- Spec 1–3、5：Tasks 1–3、8–9；源码纯净、必要补丁、既有回归均有承接。
- Spec 4、9：Tasks 2–4、9；凭据所有权、非root、旧数据/密钥迁移、实际切换均设独立验收。
- Spec 6–7：Tasks 5–7；真实排程、资格、结果、串行/幂等/碰撞、记录故障、UI与接口均有用例。
- Spec 8：Task 8；候选任何阶段失败不部署/不替换当前组合，更新后仍需用户明确部署。
- Spec 10–11：Tasks 4、9 分阶段验收；不增加排程编辑/第二调度器/账号选择/数据库/自动升级。
- 不以本计划文档代替测试证据；所有命令在实施时实际运行再记录结果。
- 本计划完成只代表可以选择实施方式，此时不迁移源码、不重启当前容器。
