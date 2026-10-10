# Task 3 review

## Spec Compliance

- ❌ Issues found: own-output plaintext history roundtrip is incomplete for a valid SSE ordering. `patches/0009-relaykit-responses-history.patch:40` unconditionally starts a new assistant message for reasoning even when the same generated turn has already emitted a tool call. This can separate tool calls from their results; see Important I1.
- ✅ The specified adapter, adapter tests, wiring, wiring test, history patch and library regressions, patch series and maintenance notes are all present. The Task 2 handoff is addressed by `extensions/internal/protocol/responses_validation.go:83` and `extensions/internal/protocol/responses_validation_test.go:13`; the old rejecting fixture now rejects malformed reasoning rather than valid plaintext (`extensions/internal/protocol/adapter_test.go:106`).
- ✅ `/v1/responses` alone is intercepted, non-POST returns 405 with Allow, one exact Bearer header is required, query/compressed-body rejection precedes shared conversion, and other paths retain the original request (`extensions/internal/responses/adapter.go:14`, `adapter_test.go:35`, `adapter_test.go:81`).
- ✅ Bridge-only wiring reuses the existing public Handler and configured request limit; the unchanged no-key return precedes the new wrapper (`extensions/cmd/server/extension.go:281`, `extension.go:284`). The wiring regression covers source mode, adapted Chat routing and forged private-header removal (`extensions/cmd/server/responses_test.go:13`).
- ✅ Function IDs/results and concurrent custom/function metadata isolation are exercised (`extensions/internal/responses/adapter_test.go:118`, `adapter_test.go:141`); JSON completed/incomplete status and stateless/state-rejection cases are exercised (`adapter_test.go:194`, `adapter_test.go:245`). SSE tests check immediate output, deferred single terminal, continuous sequence numbers, parallel argument aggregation and exact final/done equality (`adapter_test.go:258`).
- ✅ The controller-approved serializer exception remains narrow: only `content:null` and empty `role`/`quality`/`size` are accepted in reasoning. Other keys, including `signature`, `encrypted_content` even null, and opaque fields, are rejected; summary block keys are limited to type/text (`extensions/internal/protocol/responses_validation.go:123`, `responses_validation_test.go:24`). The library patch independently rejects unsupported reasoning item fields (`patches/0009-relaykit-responses-history.patch:8`).
- ✅ The library adjustment is carried by an ordered patch with three regression tests and removal conditions; the review package changes no snapshot, module identity, deployment configuration, services or account pools (`patches/0009-relaykit-responses-history.patch:72`, `patches/series:10`, `patches/README.md:121`).
- ⚠️ Cannot verify from this task diff: Task 2's complete cancellation/owner propagation, unknown/late/zero usage handling, JSON 32 MiB and SSE 32 MiB cumulative/8 MiB frame bounds. Task 3 delegates to `protocol.Serve` (`extensions/internal/responses/adapter.go:40`); the controller should retain the shared-layer review and integration evidence.
- ⚠️ Cannot verify from this task diff: pinned-source manifests/licensing/package declarations and production Go 1.26/container/SDK/console acceptance. They are cross-task deliverables, explicitly deferred by `docs/superpowers/implementation/protocol-conversion/task-3-report.md:99` and `task-3-report.md:148`.
- ⚠️ Evidence limitation: `task-3-report.md:116` names `task-3-library-vet-final.log` and `task-3-core-vet-final.log`, but both files are absent at the stated paths. This does not establish that vet failed. The controller confirmed the final Task 5 production-toolchain `check.sh` will rerun vet; no suite or vet was rerun by this review.

## Strengths

- The public adapter is a 42-line routing/authentication boundary, leaving actual mapping and transport in the shared layer (`extensions/internal/responses/adapter.go:14`). It does not introduce a second converter, account pool, HTTP loopback, tool executor or response store.
- The ordinary history test reuses the original emitted output object instead of removing serializer fields before resubmitting it (`extensions/internal/responses/adapter_test.go:213`). That protects the real own-output contract and makes the narrow inert-field exception meaningful.
- The patch preserves plaintext, text and tools in the supported reasoning-first order, including reasoning → tool → text (`patches/0009-relaykit-responses-history.patch:40`, `:50`, `:100`). The opaque/malformed tests assert rejection before next and a useful input path (`extensions/internal/protocol/responses_validation_test.go:24`).
- Supplied RED logs demonstrate actual routing/history/validation failures on earlier materializations, and the supplied final core/race/library logs are clean. The report distinguishes mock conversion results from actual upstream, SDK and container behavior (`docs/superpowers/implementation/protocol-conversion/task-3-report.md:40`, `:61`, `:81`, `:112`, `:148`).

## Issues

### Critical (Must Fix)

None found in this task package.

### Important (Should Fix)

**I1 — Reasoning after an already emitted tool call splits one assistant turn and its tool/result batch.**

- **Location:** `patches/0009-relaykit-responses-history.patch:40` (materialized `third_party/relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go:240`).
- **Trigger:** Before finish, the upstream sends a complete function-call delta, then a reasoning delta, optionally then assistant text. This is accepted by the shared stream layer (`extensions/internal/protocol/stream.go:196`, `:205`, `:249`). The pinned library allocates indices on first appearance and emits final output in that order (`extensions/third_party/relaykit/relayconvert/internal/oai_chat/to_oai_responses_stream_resp.go:487`, `:585`, `:767`, `:821`), so generated output can be `[function_call, reasoning, message]`.
- **Result:** Appending a matching function_call_output and resubmitting full history produces `assistant(tool_calls)` → `assistant(reasoning_content, content)` → `tool(tool_call_id)`. The unchanged function-call helper attaches a call to the current assistant (`extensions/third_party/relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go:367`), but the new reasoning case always appends a different assistant. The following text only merges into the new reasoning assistant (`patches/0009-relaykit-responses-history.patch:50`). Tool results are therefore separated from the assistant call batch by another assistant message, making the generated history unsuitable for Chat tool execution semantics. With two calls on either side of reasoning, even parallel calls from the same generated turn split across assistants.
- **Why existing checks miss it:** The endpoint history test starts from an ordinary JSON response, whose converter puts reasoning first (`extensions/internal/responses/adapter_test.go:213`). The stream fixture also emits reasoning first (`adapter_test.go:263`). All three patch regressions begin with reasoning before any call (`patches/0009-relaykit-responses-history.patch:72`, `:90`, `:100`). The tool-before-text regression does not cover tool-before-reasoning.
- **Fix:** In the minimal request-mapping patch, attach a reasoning item to the preceding assistant item of the same contiguous output turn when applicable, retaining its existing text/tool calls. Add a library regression for call → reasoning → text → result, and an endpoint regression that resubmits the untouched final SSE output for reversed/interleaved reasoning/tool order and asserts one assistant call batch followed by the matching results. Do not reorder previously emitted SSE indices or weaken opaque-field validation.
- **Verification:** Static tracing of the complete mapper helper and stream-order functions establishes the split; no new test source was written and no test suite was rerun under the read-only review scope.

### Minor (Nice to Have)

None found beyond the evidence limitations above.

## Checks and scope

- Reviewed the entire supplied `8954bef4a8c1ee464c8d99ca657dd8fa914fbef7..5602eb2b9a5ce53de28dc062c494f86ec41fb39e` package once, in sequential chunks after the combined tool output was truncated. Narrow line indexing used only that package; changed files were not separately reread.
- Named outside-diff risk: whether generated final stream output order is compatible with the new historical assistant merge. Checked the unchanged Responses request mapper/helper, Chat-to-Responses stream index/final-output functions, and shared pre-finish delta acceptance only for that risk. Two initial guessed converter filenames did not exist; the focused directory file list identified the correct converter paths.
- Read the brief, design spec, root AGENTS and reviewer instructions. Initial `git status --short` recorded existing `.gitignore`, task-5 brief, plan/progress docs, probe files and `upstream-convert/` changes; they were preserved. This report is the only file written by the reviewer.
- Read `task-3-route-red.log`, `task-3-history-red2.log`, `task-3-library-red.log`, `task-3-validation-red.log`, `task-3-order-red.log`, `task-3-core-final.log`, `task-3-race-final.log` and `task-3-library-final.log`. Final test logs contain passing results and no warnings; expected `[no test files]` package lines are not warnings. Missing named empty vet logs were reported to the controller instead of regenerated.

## Assessment

**Spec compliance:** Needs fixes — plaintext roundtrip does not cover valid stream output ordering.

**Task quality:** Needs fixes.

**Reasoning:** The endpoint boundary, bridge isolation and rejection rules are small and well covered. The history patch assumes reasoning arrives first, while the library intentionally preserves first-seen SSE order; fixing I1 and adding an untouched SSE-output roundtrip regression is required before this task is trusted.

Skipped: passed suites/race/vet were not rerun; production-container, SDK and real-upstream behavior were not checked here.
Risk: tool-first SSE histories can split a call/result batch until I1 is fixed.
