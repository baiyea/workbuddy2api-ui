# Docker Web Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** A fresh `docker compose up -d` starts a persistent, authenticated browser console for OAuth accounts, status and streaming chat.

**Architecture:** Embed static HTML/CSS/JavaScript in the existing Go HTTP service. Share OAuth protocol with the CLI, keep upstream tokens server-side, reuse the existing pool and chat handlers. Use named volumes and independent persisted admin/API keys.

**Tech Stack:** Go standard library, existing upstream/pool packages, native browser fetch/SSE, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-09-15-docker-web-console-design.md`

## Global Constraints

- 当前目录实现，不创建 worktree；用户已批准本设计及执行。
- Single server and port 7863; no extra browser, frontend service, database or new runtime dependencies.
- Real upstream login/verification remains a user action. Mocked verification never counts as real-account acceptance.
- No publication to an unspecified registry and no production server deployment.
- Existing CLI, API Bearer authentication and account files remain compatible.

### Task 1: Shared OAuth and safe account installation

**Files:** create `internal/oauth/client.go`, `internal/oauth/client_test.go`; adapt `cmd/login/main.go`; add focused account installation code/tests in `internal/pool` and `internal/auth`.

**Interfaces:** OAuth `New(realm string) (*Client,error)`, `Start(ctx) (state,url string,err error)`, `Poll(ctx,state) (*auth.Auth,error)`; distinguish `ErrPending` and terminal failures. Global registration helper returns allowed region options; completion validates server-fetched choices. Pool installs persisted credentials while preserving account identity and coordination with refresh.

- [x] Write a fake HTTP upstream contract test returning pending then tokens/account; assert realm, account identity, no success on missing UID or invalid redirect.
  ```go
  a, err := client.Poll(context.Background(), "opaque-state")
  if err != nil || a.UID != "account-1" { t.Fatalf("login result: %v %v", a, err) }
  ```
- [x] Run `go test ./internal/oauth ./cmd/login`; observe missing functionality before implementing.
- [x] Extract shared bounded JSON requests and envelope parsing, realm endpoint selection and per-flow cookie jar. CLI wrappers retain state-file output compatibility and tests.
- [x] Implement atomic account installation and run `go test ./internal/auth ./internal/pool ./internal/oauth ./cmd/login`.

### Task 2: Authenticated console and browser flow

**Files:** `internal/server/admin.go`, `internal/server/admin_test.go`, `internal/server/web/{index.html,app.js,style.css}`, `internal/server/handler.go`.

**Interfaces:** `Config.AdminKey`, `Config.AuthDir`; `Handler` registers `/`, `/livez` and `/admin/*`. Admin routes call existing status/models/chat functions after session validation; public `/v1` keeps Bearer checks.

- [x] Write HTTP tests for anonymous rejection, successful login Cookie/CSRF, cross-origin rejection, session-bound OAuth, secret-free responses and zero-account liveness.
  ```go
  rr := httptest.NewRecorder()
  h.ServeHTTP(rr, httptest.NewRequest("GET", "/admin/status", nil))
  if rr.Code != http.StatusUnauthorized { t.Fatalf("status=%d", rr.Code) }
  ```
- [x] Run `go test ./internal/server -run Admin`; observe failures, then implement bounded sessions and login throttling, flow expiry and serial polling. Validate UID and upstream authorization links. Keep credentials private.
- [x] Build Chinese login, overview, accounts and chat panels using local static assets. Use textContent for untrusted output; stream parser handles chunk splits, error frames, reasoning, tool-call display and abort.
- [x] Global region-required flow renders server-provided choices and submits selection before activation/trial retry. Account installation does not require restart.
- [x] Run server tests; use real browser against mock upstream to verify login, persisted account, state, streaming chat and stop behavior.

### Task 3: Startup, Compose, documentation and integration

**Files:** `cmd/server/bootstrap.go`, `cmd/server/bootstrap_test.go`, `cmd/server/main.go`, `Dockerfile`, `docker-compose.yml`, `.dockerignore`, `README.md`.

**Interfaces:** `bootstrap(cfg *Config) (adminKey string,err error)` loads environment override or generated persisted key; API key generated only for new managed deployment without explicit key. Docker enables managed mode and uses named auth/data volumes.

- [x] Write bootstrap tests asserting key uniqueness, persistence, environment/config precedence and corrupted-file failure rather than silent rotation.
  ```go
  first, err := bootstrap(cfg)
  if err != nil { t.Fatal(err) }
  second, err := bootstrap(cfg)
  if err != nil || first != second { t.Fatal("key changed across restart") }
  ```
- [x] Run focused tests RED then implement and run GREEN. Startup validates writable paths and logs only generated admin key intentionally; never upstream credentials.
- [x] Adjust Dockerfile to include resources, checksums, non-root writable named-volume directories and `/livez` probe. Compose needs no config.json or .env, builds local image on first up. Keep optional explicit configuration documented.
- [x] Run `go test ./...`, `go vet ./...`, targeted race tests and `git diff --check`.
- [x] Build/start a separate Compose project and fresh volumes; verify UI/401/liveness, restart credential persistence and non-root execution. Do not remove user containers or volumes.
- [x] Review complete diff against spec, resolve issues, record verified commands and any real-account/publishing limitations in handoff.

## Execution record

- Design approved; inline execution chosen because the auth, pool and handler changes share interfaces. No further execution-choice approval needed.
- Shared OAuth and admin CN/global flows verified with fake upstream; explicit region choice, persistence, reauthorization, cancellation, CSRF and session isolation covered.
- `go test ./...`, `go vet ./...`, six-package `go test -race`, `node --test internal/server/web_test.cjs` and `git diff --check` passed.
- Fixed existing config startup fallback: `Load` wraps ENOENT, so `errors.Is` is required. New subprocess regression starts with no config file.
- Existing ledger test assumed reset_at was always emitted; aligned assertion with documented omission when it equals until. Passed 30 consecutive runs.
- Docker linux/arm64 image built and started as isolated project `wb2api-console-smoke` on port 17863. UID/GID 10001; homepage 200; anonymous admin 401; livez 200; empty readiness 503; Docker health healthy; persisted key hash unchanged after restart.
- Local Docker registry/proxy failed initially. Downloaded official base images with temporary crane outside repository, then supplied host proxy only as build arguments. No daemon/proxy global settings changed; no new project dependency.
- In-app browser: real container bootstrap login/empty overview; mock account status, streaming answer and usage, stop, API-key reveal, logout; 390px mobile screenshot checked and logout made visible.
- Independent review completed; fixes include keeping model limits on reauth, precise re-login revival, durable pending activation and outgoing-request snapshot guard.
- Real upstream account login and actual model reply remain user acceptance. No image registry publication or remote deployment performed.
- Current master directory retained as requested, no worktree or automatic Git integration. Test image/container/volumes are separate from existing deployments.
