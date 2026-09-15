# Overlay 迁移与验收记录（2026-09-15）

## 结论与边界

源码布局已从根目录直接维护上游文件，迁移为 `upstream/` 固定快照 +
`extensions/` 新增文件 + `patches/` 有序补丁 + 独立 `console/`。本记录只证明源码、
构建和隔离 mock 验收；真实容器 `wb2api-console-smoke` 尚未切换，真实账号、OAuth、
模型消费和任务奖励均未执行。

真实旧版无法完整观察后台任务是否在途，因此正式切换必须等待用户确认维护窗口。当前
状态是“源码与 mock 已验证，实际部署待确认”，不是“已经上线”。

## 精确源码身份

- 上游仓库：`https://github.com/Sliverkiss/workbuddy2api`。
- 固定 commit：`c576b489fa22e3c156e960ee6336c4e653a0d95c`。
- `upstream/` 内容/模式摘要：
  `299e722b5dce1b17273383ebaedb277e8f2a11cce3a219383c9d4cc527e5aafb`。
- `upstream.lock` SHA-256：
  `ee478f791367f556b2f384a9609cde51683c13475cedb93e8f407200c4ae677d`。
- 当前扩展/补丁 `patch_identity`：
  `9bcbc4b66ab0ee07e2c928b965230e9c8e67fab500e47bd7e577a7948fd9f710`。
- 补丁顺序：`0001-auth-pool-consistency.patch`、
  `0002-upstream-credential-snapshots.patch`、`0003-core-wiring.patch`、
  `0004-scheduler-observation.patch`、`0005-regression-tests.patch`。

身份以仓库 URL + commit + 源码摘要为准；本机 Docker 代理地址不是源码身份。Task 9
验证前后重新计算的 `upstream/` 摘要相同。

## 删除前证据与恢复位置

Task 1 私有源码备份位于：

```text
/tmp/workbuddy2api-task1-backup.wo1TYT
```

目录权限为 `0700`，文件为 `0600`。Task 9 实际执行了 `git bundle verify` 和 gzip/tar
可读性检查：`repository.bundle` 是完整历史，`source-files.tar.gz` 可列出并解包。
备份还包含 `worktree.patch`、`index.patch`、`git-status-short.txt`、diff/stat、最近日志和
远端记录。

这个归档明确排除了 `.git/`、`.agents/`、`.build/`、`auths/`、`data/`、`.env*`、
`config.json`、私钥和备份目录。因此它是源码恢复备份，不是实际账号/状态备份；不能把
它当作真实部署切换的最终数据备份。

源码恢复方法：从 `repository.bundle` 克隆到新目录，审阅后用 `git apply --binary`
应用 `worktree.patch`；把 `source-files.tar.gz` 解到另一个空目录，挑选需要的未跟踪文件，
不要直接覆盖当前工作树。

## 原始改动到新布局的映射

删除前断言结果：

```text
PREDELETE_ASSERTIONS_OK planned_tracked=141 baseline_snapshot_exact=141
initial_modified_source=16 patch_mapped=15 handler_split=1 untracked_mapped=14
BACKUP_READABLE /tmp/workbuddy2api-task1-backup.wo1TYT
RESIDUAL_STATUS_EXACT 29
```

141 个已跟踪删除候选逐文件满足：原路径存在于原始 commit，并与
`HEAD:upstream/<原路径>` 的 blob ID、Git 模式完全相同。开始时的 residual 状态与删除
前状态完全对应；额外只有明确要求保留的未跟踪历史计划
`docs/superpowers/plans/2026-09-15-docker-web-console.md`，没有发现未承接的自定义工作。

### 开始时已修改的跟踪文件

| 原路径 | 承接位置/处理 |
| --- | --- |
| `Dockerfile` | `deploy/core.Dockerfile`、`deploy/console.Dockerfile` 与根 Compose；旧根副本删除 |
| `cmd/login/main.go` | `patches/0003-core-wiring.patch` |
| `cmd/server/main.go` | `patches/0003-core-wiring.patch`，初始化与 task runner 接线在 `extensions/cmd/server/extension.go` |
| `internal/auth/auth.go` | `patches/0001-auth-pool-consistency.patch` |
| `internal/pool/{cooldown,entry,pick,state}.go` | `patches/0001-auth-pool-consistency.patch` |
| `internal/scheduler/{scheduler,travel}.go` | 凭据快照在 0002，统一排程与结果观察在 0004 |
| `internal/server/handler.go` | 原 admin 注册/配置移到 `console/` 与 `extensions/internal/bridge/`；物化 core 恢复纯公共 Handler |
| `internal/server/handler_test.go` | `patches/0005-regression-tests.patch`，保留一秒 ledger 时间语义并接受等值 `reset_at` 省略 |
| `internal/upstream/{client,global_models,headers,report,trial}.go` | `patches/0002-upstream-credential-snapshots.patch` |

### 开始时未跟踪的自定义源码

| 原路径 | 承接位置 |
| --- | --- |
| `cmd/server/bootstrap.go`、`bootstrap_test.go` | `extensions/cmd/server/extension.go`、`extension_test.go` |
| `internal/oauth/client.go`、`client_test.go` | `extensions/internal/oauth/` |
| `internal/pool/install.go`、`install_test.go` | `extensions/internal/pool/` |
| `internal/server/admin.go` | `console/server.go`、`console/proxy.go`、`extensions/internal/bridge/oauth.go` |
| `internal/server/admin_test.go` | 对应 console/bridge 测试 |
| `internal/server/browser_preview_test.go` | `console/browser_preview_test.go` |
| `internal/server/web/{index.html,style.css,app.js}` | `console/web/` |
| `internal/server/web_test.cjs` | `console/web_test.cjs` |
| `internal/upstream/reauthorize_test.go` | `extensions/internal/upstream/reauthorize_test.go` |

### 关键实现的物化源码比较

Task 9 将源码备份解到权限受限临时目录，并把当前 overlay 物化到新的 `.build` 子目录，
使用 `git diff --no-index` 比较关键实现：

- 与开始时源码逐字相同：`cmd/login/main.go`、`internal/auth/auth.go`、
  `internal/pool/{cooldown,entry,pick,state}.go`、`internal/server/handler_test.go`、
  `internal/upstream/{global_models,headers,trial}.go` 和 OAuth 测试。
- `internal/server/handler.go` 的差异正是删除原内嵌 admin 字段、路由和 `/livez`；这些职责
  已分别由 console 和 core bridge/liveness 承接，不是业务遗漏。
- `cmd/server/main.go` 的差异是从单进程 bootstrap/admin 改为早期 core 初始化、晚期
  bridge 包装和同一 scheduler/runner 接线。
- scheduler/travel 的新增差异是 Task 5 的北京时间统一、唯一 Scheduled 回调和结构化账号
  结果观察；没有复制第二个 scheduler。
- upstream client 的后续差异把逐字段临时副本替换成一致的 `Auth.Snapshot()`，并固定
  billing fallback 快照；是原并发安全改动的更严格承接。
- OAuth client 的差异只包含 gofmt；pool install 的差异是 review 后加强 realm 身份比较；
  install/reauthorize 测试增加了相应回归覆盖。
- 原网页资产和当前 `console/web/` 的 diff 只增加自动任务页、生命周期修复和测试；原四页
  仍由 console 浏览器夹具覆盖。

## 受控删除结果

只通过精确路径 `git rm` 删除；没有使用 `rm -rf`、`git clean`、reset、reinit 或 broad
pathspec。删除了 141 个已跟踪旧布局文件：

- `cmd/` 下 18 个原跟踪文件；
- `internal/` 下 107 个原跟踪文件；
- 根 `Dockerfile`、`go.mod`、`go.sum`、`config.example.json` 和五个旧业务 shell 脚本；
- `scripts/` 下五个上游业务脚本根副本；
- 根 `.github/workflows/ai-governance.yml` 与 `build.yml` 两个会主动运行的 workflow。

另外精确删除 14 个已经映射的未跟踪旧位置文件。对应源码继续存在于
`extensions/`/`console/`；上游业务脚本、Go module、Dockerfile、示例配置和 GitHub
workflow 的纯副本仍在 `upstream/`。根只保留定制构建/acceptance 脚本，不新增 CI。

保留且未改写：`.agents/`、`.env*`、`auths/`、`data/`、`config.json`、用户文件、根
`LICENSE`、`.git` 历史、`.github` 中未在删除裁决内的内容，以及未跟踪历史计划
`docs/superpowers/plans/2026-09-15-docker-web-console.md`。

## 完整验证

按 Task 9 固定顺序执行：

1. `python3 -m unittest discover -s scripts -p test_overlay.py -v`：36/36，exit 0。
2. `python3 -m unittest discover -s deploy -p test_migrate.py -v`：8/8，exit 0。
3. `bash scripts/check.sh`：新的物化 core 19 个包通过；`go vet ./...` 通过；auth、pool、
   upstream、scheduler、taskrun、bridge 竞态测试通过；console race 通过；Node 17/17；
   物化 Python 15/15；exit 0。
4. `docker compose config --quiet`：exit 0。
5. `docker compose build --build-arg HTTP_PROXY=http://host.docker.internal:7890
   --build-arg HTTPS_PROXY=http://host.docker.internal:7890`：两个镜像构建成功；代理只在
   本机命令显式给出，代码没有 host 默认值。
6. `WB2A_ACCEPTANCE_KEEP=true HTTP_PROXY=http://host.docker.internal:7890
   HTTPS_PROXY=http://host.docker.internal:7890 bash scripts/acceptance.sh`：fresh、rebuild、
   legacy、封闭 mock 模型/SSE、任务执行记录和重启持久性通过；exit 0。保留的浏览器验收
   项目见下一节。
7. `git diff --check`：最终文档和浏览器证据写入后 exit 0；随后
   `git diff --cached --check` 同样 exit 0。

acceptance 断言 core 只有 internal mock 网络，console 额外连接 entry 网络用于 localhost
端口；两个服务 UID 10001；只有 console 映射单端口；空账号 `/livez` 200、`/healthz`
503；错误鉴权、固定路由、CSRF/owner/protocol、OAuth 11217/激活/重授权、SSE 首帧与取消、
任务资格/unknown、并发/幂等/计划合并、坏历史隔离、legacy 字节和任务记录重启保留由脚本
及全套 Go/Node/Python 测试共同覆盖。

## 浏览器 mock 验收

- URL：`http://127.0.0.1:56180/`。
- 项目：`wb2api-task4-1789476595-47332-legacy`。
- 数据：一个合成 global 账号、合成模型与任务历史；不含本机真实账号。
- 管理密钥是 acceptance 固定 mock 值，未写入本文。
- 桌面五页均正常。概览显示 1 个账号、1 healthy、0 in-flight；账号页显示 global/mock
  fixture、历史 credits 77、errors 4。API 页 Base URL 为当前 mock 的 `/v1`，点击显示得到
  mock legacy API Key，但没有复制。
- 对话页明确选择 `global:mock-model`，隔离 mock 返回 `mock-runtime-ok`，usage 为 1/1/2；
  没有访问真实模型。
- 自动任务页显示来自 mock core 的六项配置：checkin 启用且唯一时点为北京时间 04:00，
  其余禁用；已有
  checkin 记录为 skipped/global、奖励未确认、无余额。刷新后记录仍在。
- 390x844 下五页 `scrollWidth=390`，导航和退出可见；键盘 Enter 退出返回登录页，随后恢复
  viewport。
- 脱敏截图：
  [桌面任务详情](2026-09-15-overlay-task-console-desktop.jpg)（SHA-256
  `64875a672d7248b385e3e5d97dbcf4568ea9beb60ef9565a554500ff0e555b90`）和
  [窄屏任务首页](2026-09-15-overlay-task-console-mobile.jpg)（SHA-256
  `edc77e425a8779df7600802c17367c4c6eff85e8b156ade60407396463f26267`）。

浏览器完成后，已只对上述精确项目执行 `down --remove-orphans`（未加 `-v`），逐个确认
三个卷的 `wb2a.acceptance` 标签等于项目名，再单独删除 `_auths`、`_data`、`_keys`
卷。检查确认项目容器和三卷均不存在；未使用 prune。清理后真实容器仍为 running/healthy，
两个真实卷仍存在。

## 真实容器只读清点与切换状态

2026-09-15 的只读 inspect 结果：

- 容器 `wb2api-console-smoke`，ID
  `4a67ce551b49af4cc7b5370aeac359e7a7e86bb396a4aa5fa35c476c8dd9488d`；
- 镜像 ID `sha256:6ca379988253842bca21f4caaf716e669ace1ec624819597e16f96ddec7532d3`；
- 状态 running/healthy；容器 7863 映射到宿主 `0.0.0.0:17863` 和 `[::]:17863`；
- `/app/auths` 使用命名卷 `wb2api-console-smoke_auths`；
- `/app/data` 使用命名卷 `wb2api-console-smoke_data`。

inspect 没有读取或记录容器 Env、key 或数据内容。真实容器没有被停止、重建、备份或
迁移，两卷没有被挂载读取或删除。旧版后台任务不可完全观测，因此一次健康状态或无日志
不能证明没有在途任务。

正式切换仍需：用户确认停止使用/维护窗口 → 重新现场解析容器和两个挂载 → 停止精确旧
实例但不删卷 → 创建并验证最终一致性数据备份 → 在 `.env` 写入这两个实际卷名及原端口
17863 → 启动新 Compose → 只验证旧管理/API Key、已有账号状态和保存数据一致。真实登录、
模型消费和任务奖励由用户手工决定；任何挂载、备份或 key 冲突都应停止并回退旧镜像/
服务定义复用原卷。

## 已知限制

- 没有真实部署、真实 OAuth、验证码、模型消费或任务奖励证据；本文不宣称真实到账。
- Task 7 页面在初始 `active_run=null` 时不会持续轮询，因此别的会话或定时任务后来开始，
  空闲可见页可能要刷新后才显示。
- active poll 每次会打开原 active run 的详情，可能重新打开用户已经关闭的详情，或覆盖
  用户正在看的历史详情并再次滚动。
- “加载更早记录”没有 in-flight guard；快速重复触发可能用同一 cursor 请求两次并追加
  重复行。这是已登记的 deferred minor，本 Task 9 不越界修改。
- Go `TravelClaim` 的 `int64` 不能区分缺失奖励和明确零，因此零显示为未确认；正数才是
  已确认奖励。
- 历史默认保留 30 天且最多 1000 条；只裁剪终态记录。账号/合并计划数组上限各 10000，
  历史文件上限 128 MiB，超限拒绝写入而不是静默截断。
- 后台任务首版全局串行，无持久任务队列、停机补跑、网页编辑排程或单账号执行。

## 执行过程裁决（按 progress.md 时间顺序原样保留）

1. Ruling: Keep current checkout and master as explicitly approved; no worktree or branch switch — respects the user's confirmed layout migration — cost if wrong: local commits would need moving to another branch.
2. Ruling: Strengthen the Task 1 digest and Task 5 catalog sample assertions — examples otherwise miss real content/ID regressions — cost if wrong: only test maintenance.
3. Ruling: Use cancellable lifecycle in Task 6 tests and Enabled-based unavailable-time UI in Task 7 — satisfies cleanup and truthful status requirements — cost if wrong: small test/UI rework.
4. Ruling: After extensions exist, validate core in a materialized module rather than root go test ./... — root temporarily contains extension copies by migration design — cost if wrong: final layout full validation catches omitted packages.
5. Ruling: Supplied skill scripts are non-executable; invoke through bash and pass explicit artifact outputs, leaving user .agents permissions unchanged — avoids modifying user skills — cost if wrong: regenerate scratch artifacts.
6. Ruling: Real container cutover waits for safe maintenance window if in-flight work cannot be observed — Spec 9 is binding — cost if wrong: deployment remains pending, existing service unaffected.
7. Ruling: At final layout cleanup retain upstream GitHub workflows only inside upstream/.github, not active root .github/workflows — current upstream build auto-publishes daily and governance writes issues/PRs, outside this customization's requested scope — cost if wrong: own-repo CI needs explicitly adding later.
8. Ruling: Preserve existing browser api(path, data, signal) contract; Task 7's fetch-options example must become api(`tasks/${taskID}/runs`, {request_id: requestID}) — actual helper prefixes /admin and serializes data itself — cost if wrong: small UI adapter rework.
9. Ruling: Keep an early initializer hook before auth.LoadDir and a separate late wrapCore hook — fresh volumes otherwise fail before key/directory creation — cost if wrong: one extra documented main wiring hook.
10. Ruling: Order per-session OAuth control requests and logout in console, invalidate locally immediately, recheck under gate, bound independent cleanup — avoids late core flow creation after logout without a new core tombstone registry — cost if wrong: cleanup latency or revisiting the core protocol.
11. Ruling: Preserve migrated assets except necessary lifecycle compatibility fixes; clear local UI on logout failure and show cleanup error — backend failure contract changed while session is already revoked — cost if wrong: small frontend regression adjustment.
12. Ruling: Runtime admin/API overrides remain actual overrides per Spec70, applied equally in core/console after validating a common persisted base keyfile; never rewrite base/oldkey bytes. Target-versus-legacy persisted conflict still fails, bridge remains persisted-only — rejecting all different overrides would remove approved compatibility — cost if wrong: explicit env/key documentation and narrow bootstrap rework. Removing runtimeoverride returns to persistedbasevalue; Compose passes identical overrideenv to bothservices.
13. Ruling: Docker-mode config.json-only api_key conflicting with persistedbase and no sharedWB2A_API_KEY fails with instruction to use sharedoverride; do not silently ignore config or allow servicekey divergence. Empty/matchingconfig okay, source semantics unchanged — protects existing API clients duringmigration — cost if wrong: user explicitly moves configoverride intoComposeenv.
14. Ruling: Isolated acceptance core connects only internal mock network; console also connects ordinary entry network to expose localhost port while fixedproxy targets core only — worker reproduced Docker Desktop not publishing ports for internal-only container (healthyinside, missingNetworkSettings.Ports) — cost if wrong: narrow acceptance-network rework, never givecore realupstream egress.
15. Ruling: Adapt old school_test.go nil-Pool script fixtures through patch 0005 with real eligible CN test pool and expanded command fake, while new school.go behavior lives in 0004 — no eligible account must not launch a child, so old nil-Pool expectations contradict the spec — cost if wrong: narrow original-test maintenance, no production test bypass.
16. Ruling: Actual Scheduler.Run converts now to the same Beijing zone used by TaskCatalog before calling original nextWake — spec requires displayed and actual schedules agree even on non-Beijing hosts — cost if wrong: explicit source-mode timing change; retain timer/ordering and add targeted zone regression. Short-lived private collector under mutex is approved, no generic event bus.
17. Ruling: TravelClaim zero remains Reward=null because its existing int64 API collapses missing reward and explicit zero; positive explicit reward is recorded, successful claim status remains success — avoids inventing certainty or expanding upstream API for telemetry — cost if wrong: genuine zero reward is displayed unknown. Task 7 should label amounts confirmed rewards, not total balance delta.
18. Ruling: Close the existing task_common.load_auth read-only file with a with-open block in patch 0004 — new real-loader tests exposed the existing ResourceWarning; two-line resource hygiene avoids warning suppression or test bypass — cost if wrong: tiny upstream patch context to maintain, no parsing or credential behavior change.
19. Ruling: Add one package-private schedulerNow=time.Now seam used only by Run to test real callback/timezone wiring without waiting for an hour — Go 1.23 build cannot use testing/synctest — cost if wrong: one mutable test hook; tests must not run parallel and must await scheduler exit before restoring it, with no probabilistic expired-timer/cancel race.
20. Ruling: Nonzero child exit preserves explicit per-account events, marks only eventless accounts failed, and returns a fixed process error. Task 6 treats non-nil execute error as additional overall failure evidence: partial_failure if any success or unknown account exists, otherwise failed; cancellation remains interrupted and panic remains unknown — school exits 2 for one failed account, which must not erase another account's success — cost if wrong: small aggregate-status adjustment; never report overall success on nonzero exit.
21. Ruling: Store total bound is 1000 including active, but only terminal records may be pruned; impossible all-active overflow rejects writes instead of deleting active records. Recovery FinishedAt is observation time, DurationMS=null and fixed unknown-result note; array safety bounds are explicit constants and reject rather than silently truncate — preserves truthful recovery and bounded storage — cost if wrong: bounds may need deliberate adjustment for unusually large pools.
22. Ruling: Task 7 request IDs use crypto.randomUUID when available, otherwise 16 crypto.getRandomValues bytes encoded as 32 hex characters; no Math.random fallback or dependency — default server HTTP may lack secure-context-only randomUUID, while getRandomValues is supported in insecure contexts — cost if wrong: tiny browser compatibility helper. Verify unavailable-randomUUID branch in Node tests. Primary browser docs checked: https://developer.mozilla.org/en-US/docs/Web/API/Crypto/randomUUID and https://developer.mozilla.org/en-US/docs/Web/API/Crypto/getRandomValues .
23. Ruling: Extend updater dirty/input identity coverage to .dockerignore plus all actual candidate inputs, despite brief's shorter exact path list; document-only edits remain allowed — candidate builds consume .dockerignore and committed concurrent input changes can evade dirty-only checks — cost if wrong: slightly stricter update preflight, no new deployment behavior. Host-local proxy is explicit invocation configuration only, never a portable default.
