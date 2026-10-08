# Upstream patches

> 注意：上游 Sliverkiss/workbuddy2api 仓库已删除。本仓库自行维护该冻结基线与补丁，
> `overlay.py update` 不可用；新增能力优先放 `extensions/`，修改既有文件通过补丁。

Baseline: Sliverkiss/workbuddy2api commit
`c576b489fa22e3c156e960ee6336c4e653a0d95c`. Apply only through
`python3 scripts/overlay.py prepare --output ABS_NEW_DIRECTORY`; never edit
`upstream/`. New source and tests live in `extensions/`, not in these patches.
`series` is the explicit application order: 0001, 0002, 0003, 0004, 0005.
For a deliberate upstream candidate, run
`python3 scripts/overlay.py update --ref COMMIT_OR_TAG`; it keeps the current
snapshot and lock until the candidate passes `scripts/check.sh` and isolated
mock acceptance. Review every patch against the resolved commit before
committing the resulting `upstream/` and `upstream.lock` changes.

| Patch | Purpose and old files changed | Verification after materializing |
| --- | --- | --- |
| 0001-auth-pool-consistency | `internal/auth/auth.go`: locked snapshots/save/replace, stale activation guard and persisted pending marker. `internal/pool/{cooldown,entry,pick,state}.go`: observed-credit flag, pending guards for ordinary/model-exempt/cooldown-fallback selection and status. Install stays an extension and persists before publishing while preserving the live Auth identity. | `go test ./internal/auth ./internal/pool ./internal/upstream` and their race tests |
| 0002-upstream-credential-snapshots | `internal/upstream/{client,global_models,headers,report,trial,travel}.go`: one refresh snapshot including explicit realm, stale-refresh rejection if either token changes, private per-request snapshots, pending chat rejection, cancellation-aware OAuth post-login requests, snapshot retained across billing fallback. `internal/scheduler/{scheduler,travel}.go`: four credential presence reads use snapshots (checkin, keepalive, activity, travel); no scheduler algorithm changes. | `go test ./internal/upstream ./internal/bridge`; `go test -race ./internal/auth ./internal/pool ./internal/upstream ./internal/scheduler ./internal/bridge` |
| 0003-core-wiring | `cmd/server/main.go`: early initialization before LoadDir, late wrapper after existing dependencies/context, wrapped config-not-found handling through errors.Is; opt-in Scheduled callback captures the local Runner before scheduler.New, then initializes history/Runner before scheduler.Run. `cmd/login/main.go`: reuse OAuth client, strict complete-account validation and pending classification. No admin implementation enters the original public Handler. | `go test ./cmd/server ./cmd/login ./internal/bridge ./internal/oauth`; `go test -race ./internal/taskrun ./internal/scheduler ./internal/bridge ./cmd/server` |
| 0004-scheduler-observation | `internal/scheduler/{scheduler,travel,school}.go`: optional scheduled callback, Beijing-time scheduling, short-lived per-instance observations and context-bound scripts using private CN snapshots. `scripts/{school_open_day_2026,task_runner}.py`: fixed structured events at existing result branches, preserving activity algorithms and limits. `scripts/task_common.py`: close the existing read-only auth handle. | `go test ./internal/scheduler`, `go test -race ./internal/scheduler`, `python3 -m unittest discover -s scripts -p test_task_events.py -v` |
| 0005-regression-tests | `internal/server/handler_test.go`: ledger reset time can be omitted when it equals until; accept either representation but retain the one-second timing assertion. `internal/scheduler/school_test.go`: existing fixed-command/dispatch tests use a valid CN pool and context/output-aware fake; empty pools no longer launch a child. Production ledger format is unchanged. | `go test ./internal/server -run TestStatusRateLimitedModelsLedger`, scheduler tests and full suite |

Remove each patch only when the pinned upstream supplies the corresponding
behavior and the named regression tests pass without that patch. For 0003,
upstream must provide equivalent early/late integration hooks and shared OAuth
validation; for 0005, upstream's test must accept its existing omitted reset_at
representation. Re-evaluate patches individually on an explicit upstream update.

Remove 0004 when upstream supplies equivalent scheduling callbacks, safe account
snapshots and explicit task results. A script exit code is not a per-account
success: nonzero exit preserves explicit events, fails accounts without events,
and returns a fixed process error for the run aggregator. Only explicit reward
fields are collected. Go TravelClaim cannot distinguish omitted reward from
zero, so both remain null. Ordinary script output is replaced with a bounded
omission summary; no process-wide logger is captured. The original auth loader
must remain read-only (the extension test rejects write access).

Task 2 wiring handoff: `initializeCore(*Config) error` validates opt-in
`WB2A_BRIDGE_KEY` and creates account/state directories before loading accounts.
`wrapCore(ctx,cfg,p,up,sch,public,tasks,history,taskError)` uses the same pool/upstream/scheduler; absent
bridge key returns the identical source-mode public handler. Task 4 extends the
early hook with persisted keys and Docker opt-in, not a second pool or scheduler.
Build metadata comes from linker variables `main.upstreamCommit` and
`main.patchIdentity`; empty source-build values mean unknown.

Task 6 wiring: `coreTaskSchedule(&tasks)` remains nil without Docker/bridge opt-in.
`newCoreTasks(ctx,cfg,sch)` opens `tasks/runs.json` alongside the pool state and
binds the same scheduler's catalog/executor to the core lifecycle. Corrupt history
leaves the opt-in callback installed but skipping execution, and passes TaskError
to the bridge without stopping the public handler. Task 7 owns HTTP task routes
and their 503 mapping. Remove this part of 0003 only when upstream provides an
equivalent durable single-runner hook, including fail-closed scheduled execution.
