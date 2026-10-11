# Final correction wave report

Status: implementation and native covering checks complete; **Linux full check, Python/Compose, fresh images and actual console/core/mock acceptance with both pinned SDKs remain OPEN** while the user starts Docker Desktop. This report is not integration approval. Root owns scoped re-review and completion audit.

Production implementation: `851130dcb2a8081b8b808b6aeca52c1722ba61fa`. A follow-up commit adds the explicit tool-input overflow/deferred-failure regression, fixes README table placement and selects the user message in gateway media assertions (global routing prepends a system message). No production behavior changed in that follow-up.

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

## Integration environment / remaining gate

Read `task-5-environment-notes.md`, `task-5-report.md` and `linux-runner-context.json` before preparation. Prepared SDK Linux image/verification volume are reused once available. Docker preflight currently fails with missing `npipe:////./pipe/dockerDesktopLinuxEngine`; no Docker process was started or restarted by this agent. User elected to start Docker themselves.

Export authoritative committed LF source with `git -c core.autocrlf=false archive`; mount the existing task-owned volume at its identical daemon mountpoint, use its `tmp` directory for TMPDIR, host networking/socket, Bash `-c`. Run fresh required scripts/deploy/desktop Python, full `scripts/check.sh`, server race, Compose config, and `scripts/acceptance.sh` with both pinned interpreters and **newly built** images. Final source/patch/image identities and successful cleanup remain pending. Existing task-owned verification volume must remain for root.

## Self-review, callers and limits

- Checked all upstream Stream/Aggregate call sites, Handler success/account/close path, stats read behavior, protocol Serve/stream/usage validation, RelayKit media/image and tool lifecycle code, both SDK scripts, mock state and acceptance call chain. Original OpenAI/no-bridge callers do not set the private callback and retain the old path.
- Original `upstream/`, `upstream.lock`, vendored RelayKit bytes, `upstream-convert/`, production Compose and actual services are unchanged. Core changes to original files use ordered patches 0012/0013 with removal/maintenance notes. No new dependency/converter/pool/scheduler/service/file store was added.
- Host sequencing tradeoff is documented in README: Claude tool arguments stream immediately; text/thinking arriving after tools is delayed until all arguments are complete. Deferred state shares the original cumulative cap; cancellation/read failure suppresses queued text and success. This preserves valid block lifecycles at the cost of interleaved text latency.
- Inline resolution is limited to bounded self-contained content (hard encoded source cap 32 MiB plus normal request/converted body cap). No remote file fetch, Files service or generated JSON repair. Best-effort log/cost parsing skips metric lines beyond 64 KiB, while public usage validation continues through the larger protocol bound.
- Preserved user's `.gitignore`, untracked deferred/probe material and `upstream-convert/`, plus root audit/ledger. Staged only owned paths. No branch/worktree, real upstream call, Git push, image publication, deployment, actual-service restart or native installer work.

**Not complete:** Linux integration and fresh image identities, scoped re-review and root completion audit remain separate open gates. Mock/native successes do not establish real model capability.
