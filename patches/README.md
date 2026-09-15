# Upstream patches

Baseline: Sliverkiss/workbuddy2api commit
`c576b489fa22e3c156e960ee6336c4e653a0d95c`. Apply only through
`python3 scripts/overlay.py prepare --output ABS_NEW_DIRECTORY`; never edit
`upstream/`. New source and tests live in `extensions/`, not in these patches.
`series` is the explicit application order; 0004 is reserved for scheduler
observation in Task 5.

| Patch | Purpose and old files changed | Verification after materializing |
| --- | --- | --- |
| 0001-auth-pool-consistency | `internal/auth/auth.go`: locked snapshots/save/replace, stale activation guard and persisted pending marker. `internal/pool/{cooldown,entry,pick,state}.go`: observed-credit flag, pending guards for ordinary/model-exempt/cooldown-fallback selection and status. Install stays an extension and persists before publishing while preserving the live Auth identity. | `go test ./internal/auth ./internal/pool ./internal/upstream` and their race tests |
| 0002-upstream-credential-snapshots | `internal/upstream/{client,global_models,headers,report,trial,travel}.go`: one refresh snapshot including explicit realm, stale-refresh rejection if either token changes, private per-request snapshots, pending chat rejection, cancellation-aware OAuth post-login requests, snapshot retained across billing fallback. `internal/scheduler/{scheduler,travel}.go`: four credential presence reads use snapshots (checkin, keepalive, activity, travel); no scheduler algorithm changes. | `go test ./internal/upstream ./internal/bridge`; `go test -race ./internal/auth ./internal/pool ./internal/upstream ./internal/scheduler ./internal/bridge` |
| 0003-core-wiring | `cmd/server/main.go`: early initialization before LoadDir, late wrapper after existing dependencies/context, and wrapped config-not-found handling through errors.Is. `cmd/login/main.go`: reuse OAuth client, strict complete-account validation and pending classification. No admin implementation enters the original public Handler. | `go test ./cmd/server ./cmd/login ./internal/bridge ./internal/oauth` |
| 0005-regression-tests | `internal/server/handler_test.go`: ledger reset time can be omitted when it equals until; accept either representation but retain the one-second timing assertion. Production ledger format is unchanged. | `go test ./internal/server -run TestStatusRateLimitedModelsLedger` and full suite |

Remove each patch only when the pinned upstream supplies the corresponding
behavior and the named regression tests pass without that patch. For 0003,
upstream must provide equivalent early/late integration hooks and shared OAuth
validation; for 0005, upstream's test must accept its existing omitted reset_at
representation. Re-evaluate patches individually on an explicit upstream update.

Task 2 wiring handoff: `initializeCore(*Config) error` validates opt-in
`WB2A_BRIDGE_KEY` and creates account/state directories before loading accounts.
`wrapCore(ctx,cfg,p,up,sch,public)` uses the same pool/upstream/scheduler; absent
bridge key returns the identical source-mode public handler. Task 4 extends the
early hook with persisted keys and Docker opt-in, not a second pool or scheduler.
Build metadata comes from linker variables `main.upstreamCommit` and
`main.patchIdentity`; empty source-build values mean unknown.
