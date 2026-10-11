# Final correction wave report

Status: **DONE — correction implementation and all required native/Linux/new-image/two-SDK isolated integration gates passed.** Root independently closed the scoped code-review gate on reviewed `3f4e6be`; root completion audit and final volume cleanup remain root-owned.

Production implementation: `851130dcb2a8081b8b808b6aeca52c1722ba61fa`. Follow-up `3f4e6be3506dc9b9f283f148c9df8f210fd9cebf` adds the explicit tool-input overflow/deferred-failure regression, fixes README table placement and selects the user message in gateway media assertions (global routing prepends a system message). No production behavior changed in that follow-up.

## Seven findings and maintained coverage

| Finding | Correction | Covering evidence |
| --- | --- | --- |
| 1: termination evidence lost | A private context key installs an in-process raw response callback. Original Handler calls it before legacy Stream/Aggregate and the read-ahead stats reader. Shared frame validation now sees malformed data, DONE, transport errors and EOF directly. Ordinary validated payloads are compacted into line frames for existing aggregation, preserving valid multiline SSE and finish semantics. | `TestProtocolRawRealTerminal`, `AllTerminationAndBounds`, `MultilineDataParity`: both protocols, JSON/SSE, invalid post-finish/DONE, missing finish, read failure, residual frame; valid normal EOF with finish passes. |
| 2: raw buffering unbounded | Raw copy reads in 32 KiB chunks into the existing 8 MiB frame/32 MiB cumulative guard before any aggregation. The ordinary capture stores only validated compact frames with its own cap. Raw stats reads exactly the supplied chunk and retains at most 64 KiB for best-effort metric lines. Raw body closes and request cancels on failure; existing output caps remain. | Both-protocol/both-mode total/frame tests assert bytes consumed are at most cap + one 32 KiB read and body was closed. Explicit fragmented Claude tool-input overflow and deferred-text read failure also pass. |
| 3: partial cumulative usage overwritten | `AggregateValidated` reuses Aggregate but merges explicitly present usage fields, retaining omitted/null prior fields and allowing zero to replace a value. Legacy Aggregate keeps its prior behavior. | `TestProtocolRawRealPartialUsage`, `UsageParity`: ordinary and streamed input 5/output 0/explicit total 12, both formats. |
| 4: invalid Claude final tool JSON | Per-tool bounded argument accumulation validates a complete non-null JSON object before any success terminal. Responses continues carrying raw strings, including empty/nonobject/incomplete strings. | `ClaudeStreamArguments`, `ToolArgumentSemantics`, `ClaudeToolInputBoundAndDeferredFailure`; valid fragmented objects and SDK aggregation pass. |
| 5: interleaving loses tool fragments | Existing RelayKit remains the converter. Once Claude tool blocks begin, tool deltas stay live and text/thinking deltas are deferred until arguments finish; then deferred components are fed to RelayKit in their relative order before terminal emission. | `ClaudeInterleavedTools`; both SDKs exercise parallel call_a/call_b, intervening and mixed-chunk text/reasoning, final aggregate and generated-history return. Native mock observation verifies actual paired outbound IDs, names, JSON and results. |
| 6: inline files unavailable | Public `SetMediaResolver/GetBase64Data` resolver accepts bounded self-contained base64 text/JSON/PDF, checks MIME/base64/UTF-8, and performs no network or storage work. | `InlineMediaPayloads` verifies outgoing text and PDF; malformed encoding, non-UTF-8 text, unsupported MIME/remote URL fail before transport. Native gateway observation verifies actual outgoing payloads. |
| 7: image detail lost | Tiny RelayKit overlay preserves sibling detail in Chat `{url,detail}`; host admission permits only low/high/auto with a field-path error otherwise. | `ImageDetailValues` checks all supported values at the actual outgoing Handler boundary and invalid/null/nonstring rejection; native mock observations pass. |

Minor: promoted paired reasoning/text/tool/result history observations to permanent real Handler tests and SDK-generated outbound assertions in the existing mock gateway helper. Gateway only requires an SDK's generated history when that interpreter was enabled; normal acceptance without SDKs remains supported.

## TDD and actual native verification

All logs below are under this directory. Native toolchain is Go **1.27.2 windows/amd64**; this does not substitute for production Go 1.26 Linux validation. Native SDK venv uses Python **3.12.12**, anthropic **0.67.0**, openai **2.6.1**, httpx **0.28.1**.

| Command / stage | Result / evidence |
| --- | --- |
| Fresh `overlay.py prepare` red2, real Handler review reproductions | RED, exit 1: all seven groups fail, paired history passes; `final-fix-red.log`. |
| Expanded maintained `TestProtocolRaw*` against unmodified baseline materialization | RED, exit 1; `final-fix-expanded-red.log`. |
| Self-review multiline framing test against green4 | RED, exit 1 (ordinary valid multiline returned 502); `final-fix-multiline-red.log`. Corrected by compacting validated payloads, no new protocol mapper. |
| Fragmented tool total cap/deferred failure on baseline | RED, exit 1: 33,829,391 bytes consumed before 502; `final-fix-tool-bound-red.log`. Corrected version GREEN, exit 0; `final-fix-tool-bound-green.log`. |
| `python -m unittest discover -s deploy -p test_mock_upstream.py -v` | New parallel fixture RED then GREEN, 2 tests; `final-fix-mock-red.log`, `final-fix-mock-green.log`. |
| Fresh green5 `go test ./internal/server ./internal/protocol ./internal/anthropic ./internal/responses -count=1` | All four packages PASS; `final-fix-green5.log`. Includes all maintained termination/media/usage/argument regressions and existing affected suites. Later test-only overflow assertion separately GREEN. |
| green5 nested RelayKit `go test ./...` / `go vet ./...` | Both exit 0; `final-fix-library.log`, `final-fix-library-vet.log`. |
| green5 core `go vet ./internal/server ./internal/protocol ./internal/anthropic ./internal/responses ./internal/upstream` | Exit 0; `final-fix-core-vet.log`. |
| green5 `go test -race ./internal/server ./internal/protocol ./internal/anthropic ./internal/responses ./internal/upstream` | All 5 packages PASS; `final-fix-native-race.log`. |
| Pinned native venv `python scripts/check_anthropic_sdk.py` | All existing unknown/late/zero/text/tool fixtures PASS; `final-fix-native-sdk-fixtures.log`. |
| Native localhost Python mock + real Go Handler + both actual SDK checkers | Both ordinary/raw/high-level/parallel/interleaved/generated-history suites PASS; `final-fix-native-sdk-gateway2.log`. Diagnostic source `final-fix-native-sdk_test.go` and Go overlay `final-fix-native-sdk-overlay.json`. |
| Same native diagnostic with actual mock outbound assertions | Schema JSON/SSE, SDK-generated paired histories, inline text/PDF and low/high/auto outgoing payloads PASS; `final-fix-native-sdk-observation2.log`. No console/auth-management/container claim from this diagnostic. |
| `git diff --check` | Exit 0 before source commit. |

Diagnostic-only false starts are retained: first native fixture incorrectly used the CN `/v2/chat/completions` path against a global-only mock (404), fixed by explicitly synthetic global account/local global URLs; first outbound diagnostic used acceptance's `legacy-api` instead of native `fixture-api` (401), fixed only in the diagnostic substitution. No product retry or weakened assertion was added. The media assertion now selects the actual user message rather than assuming index zero, retaining full content equality.

## Integration environment and resolved prerequisite

Read `task-5-environment-notes.md`, `task-5-report.md` and `linux-runner-context.json` before preparation. Initial Docker preflight failed with missing `npipe:////./pipe/dockerDesktopLinuxEngine`; no engine was started or restarted by this agent. The user elected to start Docker; root then verified daemon **29.1.3** availability and authorized continuing the already-approved isolated gate.

Used exact reviewed **3f4e6be3506dc9b9f283f148c9df8f210fd9cebf**, containing production fix 851130d, exported with `git -c core.autocrlf=false archive`. Source archive `task-5-source-final-fix-3f4e6be.tar` SHA-256: `82f14e9746e790ba4d59b27fece82637c4067e0b9d3f8f4ea69d6de875c5d316`. Windows checkout Docker packaging is not claimed.

Runner `wb2a-protocol-final-3f4e6be` used prepared image `wb2a-protocol-sdk-checkenv:local`, host networking, Docker socket, existing labelled volume at its identical daemon mountpoint `/var/lib/docker/volumes/wb2a-protocol-verify-f2de220763d9/_data`, and `TMPDIR` at its `tmp` child. Non-login Bash invoked saved `final-fix-runner.sh`; source and logs were fresh children `final-fix-3f4e6be` and `final-fix-3f4e6be-logs`. Both SDK interpreters were `/usr/local/bin/python`. Per-build proxy only: `WB2A_BUILD_HTTP_PROXY`/`WB2A_BUILD_HTTPS_PROXY=http://host.docker.internal:7890`. No global proxy/config/environment was changed and `WB2A_ACCEPTANCE_SKIP_BUILD` was not set.

### Actual final Linux and container results

Environment verified: **Go1.26.9 linux/amd64**, **Python3.12.15**, anthropic **0.67.0**, openai **2.6.1**, httpx **0.28.1**. Authoritative exit table and full outputs are in [final-fix-linux-logs/](final-fix-linux-logs/); outer runner log is `final-fix-linux-runner.log`. Every listed gate exited **0**, and the runner itself exited **0**.

| Final command / gate | Actual result |
| --- | --- |
| `python -m unittest discover -s scripts -p 'test_*.py' -v` | 44 PASS |
| `python -m unittest discover -s deploy -p 'test_*.py' -v` | 15 PASS, including parallel/interleaved mock regression |
| `python -m unittest discover -s desktop/scripts -p 'test_*.py' -v` | 11 PASS |
| `bash scripts/check.sh` | Independent RelayKit/core test+vet; all 9 existing critical core race packages; console race; Node 33/33; task Python 15/15 PASS |
| Additional fresh materialization then `go test -race ./internal/server` | PASS, including raw Handler regressions |
| `docker compose --env-file /dev/null -f docker-compose.yml config --quiet` | PASS |
| `python scripts/check_anthropic_sdk.py` | All pinned Linux in-memory text/tool/unknown/late/zero usage fixture contracts PASS |
| Both pinned SDK interpreter variables + `bash scripts/acceptance.sh` | **Fresh builds/rebuilds and actual console/core/mock acceptance PASS**, no image reuse shortcut |
| Final image inspection / source overlay identity | Both linux/amd64 IDs and expected patch identity captured |
| `git diff --check` | PASS after evidence extraction |

Actual acceptance finished at synthetic `http://127.0.0.1:59459/` and project `wb2api-task4-1791682724-14338` / `-legacy`. Both SDKs passed create/raw/high-level final aggregation/usage, function/custom or Claude tools, parallel call IDs and fragmented arguments, interleaved text/reasoning, and generated-history return. The subsequent actual mock observation asserted full Handler schema JSON/SSE, both SDK-generated paired reasoning/text/tool/result histories, inline text/PDF and image low/high/auto. Existing management auth/CSRF/origin/trusted conversation, separate owner logout and public Responses disconnect cancellation passed, as did all existing startup/key override/unoverride/permissions/timezone/persistence/OpenAI/task checks. This is actual built-container evidence, separate from native smoke and in-memory fixtures; no real upstream was used.

### Source, runtime, final images and notices

- Core tag: `wb2api-final-fix-3f4e6be-core`; final ID **`sha256:5fe4573e7a38fab434b3ec6628145aba6c9c20e1ff05b72614c246baa7b04d5e`**, linux/amd64.
- Console tag: `wb2api-final-fix-3f4e6be-console`; final ID **`sha256:a2919dceb8330b5cf3f66ae71a97d8724c0414878434aa7ab282fee9fa52800b`**, linux/amd64.
- Source/runtime patch identity: **`d5bf242ac043177907aa10aaa696f3b4ee941a139ca50cb147b7986ac7417675`**. Acceptance asserted `/internal/v1/info` equals the source upstream commit and patch identity. An additional offline final-image binary inspection confirmed the embedded identity; binary SHA-256 `4a2dc8200fe1214497d584aaa05e4c296ab02effb51d658ebd79de8857b7589f` (`final-fix-final-binary-identity.log`).
- Actual final-image read with `docker run --rm --network none --entrypoint python3 CORE ...` verified LICENSE (34,523 bytes), NOTICE (2,026), THIRD-PARTY-LICENSES.md (33,787), SOURCE.json (37,493), all nonempty and SOURCE commit exactly `0f38a14a5e8e322ea741478b0e49878be3130e1d`. Exact SHA-256 values are retained in `final-fix-final-notices.log`. This reads packaged files, not Dockerfile text.
- Acceptance's normal cleanup completed. Independent project-label and exact project-name filters found no remaining acceptance containers, networks or volumes; standalone smoke used the same fresh project. The exited runner was verified exit 0, logs copied, then that exact task-owned runner removed. `final-fix-cleanup.log` records the checks. Existing labelled verification volume `wb2a-protocol-verify-f2de220763d9` remains for root and is **safe for root to remove now**. Prepared images/toolchains and all workspace evidence were retained.

## Self-review, callers and limits

- Checked all upstream Stream/Aggregate call sites, Handler success/account/close path, stats read behavior, protocol Serve/stream/usage validation, RelayKit media/image and tool lifecycle code, both SDK scripts, mock state and acceptance call chain. Original OpenAI/no-bridge callers do not set the private callback and retain the old path.
- Original `upstream/`, `upstream.lock`, vendored RelayKit bytes, `upstream-convert/`, production Compose and actual services are unchanged. Core changes to original files use ordered patches 0012/0013 with removal/maintenance notes. No new dependency/converter/pool/scheduler/service/file store was added.
- Host sequencing tradeoff is documented in README: Claude tool arguments stream immediately; text/thinking arriving after tools is delayed until all arguments are complete. Deferred state shares the original cumulative cap; cancellation/read failure suppresses queued text and success. This preserves valid block lifecycles at the cost of interleaved text latency.
- Inline resolution is limited to bounded self-contained content (hard encoded source cap 32 MiB plus normal request/converted body cap). No remote file fetch, Files service or generated JSON repair. Best-effort log/cost parsing skips metric lines beyond 64 KiB, while public usage validation continues through the larger protocol bound.
- Preserved user's `.gitignore`, untracked deferred/probe material and `upstream-convert/`, plus root audit/ledger. Staged only owned paths. No branch/worktree, real upstream call, Git push, image publication, deployment, actual-service restart or native installer work.

**Implementer work complete.** All required covering and final integration gates passed with fresh images. Scoped re-review was independently closed by root; root owns the final completion audit and labelled verification-volume cleanup. No push, image publication, deployment, actual-service restart, real-model claim or native installer validation is implied.
