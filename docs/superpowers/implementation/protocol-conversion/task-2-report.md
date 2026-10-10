# Task 2 — shared RelayKit transport

Base: `728f5a0784af77b58b2cb134c3fbd8682fd5d791`. Implemented in the current checkout without creating branches/worktrees. No public routing changed.

## Delivered

- Added `extensions/internal/protocol/{adapter,stream,usage,responses_validation}.go` and behavior tests. `Serve` and `WriteError` retain the exact brief signatures.
- Uses public RelayKit request/response/stream registry APIs and one fresh `convmeta.Values` per request. The same values restore Responses custom tools without leaking to a later request. Both the safe conversion policy and final aggregate diagnostics are checked.
- Validates UTF-8, a single non-null JSON object, known top-level fields, booleans and required model/content values; supplements loose DTO fields with state/tool/content shape checks. Raw and converted bodies share the configured limit; max output maps to existing `max_tokens`.
- Preserves request context/owner and cancellation; rebuilds path/body and only Content-Type/Authorization headers; only the explicit trusted conversation argument becomes `conversationId`.
- Captures JSON responses within 32 MiB; validates status, one valid choice, message, finish reason and usage; sanitizes upstream error bodies and headers.
- Converts SSE synchronously with 8 MiB/frame and 32 MiB/stream limits. Deltas flush before Handler returns. Finish and usage stay out of content conversion; validated normal EOF triggers synthetic finish then public Finalize. Mixed Claude reasoning/text/tools chunks are split before conversion.
- Tracks last explicit nullable input/output/total counts, including zero. Missing later fields retain prior values; unknown remains null; total is summed only when never supplied and both operands are known. Public serialized usage replaces RelayKit integer defaults.
- Truncation, invalid chunks, data after DONE, choices after finish, cancellation and write/flush failures cancel the downstream Handler context and produce no success terminal. Responses normal and error events carry continuous sequence numbers. length/content_filter retain the library's incomplete status.
- Per controller authorization, updated only the existing `0008-relaykit-module.patch` and its README explanation to include the indirect dependencies required by real relayconvert imports. No upstream or third-party originals changed.

## TDD evidence

All commands ran from the repository root. Go executable: `.build/dev-tools/go/bin/go.exe` (Go 1.27.2, Windows/amd64; gcc available). Logs are in this report's directory and use the `task-2-*.log` filenames below.

Each iteration materialized a previously absent directory with `python scripts/overlay.py prepare --output PATH`; production edits remained in extensions, never solely in a materialized directory.

| Batch | Fresh overlay | Exact test arguments after `go.exe -C PATH` | Observed result |
| --- | --- | --- | --- |
| Request RED | `.build/protocol-task2-red-request` | `test -mod=mod ./internal/protocol` | Failed behavior assertions: forged headers/path remained, no mapped max_tokens/trusted ID, converted limit bypass; malformed requests reached Handler. `task-2-red-request.log`. |
| Request GREEN | `.build/protocol-task2-green-request` | `test -mod=mod ./internal/protocol` | PASS, 0.734s. `task-2-green-request.log`. |
| JSON RED | `.build/protocol-task2-red-json` | `test -mod=mod ./internal/protocol -run 'TestUnknownUsage\|TestNonStreaming\|TestJSONResponse'` | Failed missing public nullable usage, raw error/invalid response acceptance, oversized response not canceled. `task-2-red-json.log`. |
| JSON GREEN | `.build/protocol-task2-green-json` | `test -mod=mod ./internal/protocol` | PASS, 0.886s. `task-2-green-json.log`. |
| SSE RED | `.build/protocol-task2-red-stream` | `test -mod=mod ./internal/protocol -run 'TestFirstDelta\|TestTerminalWaits\|TestLateZero\|TestPartialUsage\|TestStreamUnknown\|TestTruncated\|TestIncomplete\|TestWriteAndFlush\|TestStreamTotal\|TestMixedClaude'` | Failed real-time target events/flush, final usage, invalid stream handling, write/flush cancellation, capacity and mixed delta preservation. `task-2-red-stream.log`. |
| SSE GREEN | `.build/protocol-task2-green-stream` | `test -mod=mod ./internal/protocol` | PASS, 0.921s. `task-2-green-stream.log`. |
| Loose-field RED | `.build/protocol-task2-red-validation` | `test -mod=mod ./internal/protocol -run 'TestResponsesLoose\|TestRequestScoped\|TestOriginal\|TestCanceled'` | Ten malformed loose-field/history/schema/tool cases reached Handler. Custom-tool isolation, original limit/UTF-8 and cancellation characterization cases already passed. `task-2-red-validation.log`. |
| Loose-field GREEN | `.build/protocol-task2-green-validation` | `test -mod=mod ./internal/protocol` | PASS, 0.980s. `task-2-green-validation.log`. |
| Error-sequence RED | `.build/protocol-task2-red-sequence` | `test -mod=readonly ./internal/protocol -run TestResponsesErrorSequence` | FAIL: `event error lacks sequence 3`. `task-2-red-sequence.log`. |

The first request test invocation without `-mod=mod` failed dependency setup (`updates to go.mod needed`); that is **not** the behavioral RED evidence. The actual behavior failures above followed a successful dependency load. The final module patch eliminates the need for writable-module verification.

## Final verification

The brief's `.build/protocol-check-2` already existed without this package. `overlay.py prepare` refused to overwrite it. It was preserved; verification instead used the fresh `.build/protocol-check-2-final` materialized after the final edits and dependency patch. An attempted test against the preexisting directory failed package lookup and is not counted as product verification.

```powershell
python scripts/overlay.py prepare --output .build/protocol-check-2-final
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-2-final test -mod=readonly ./internal/protocol
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-2-final test -mod=readonly -race ./internal/protocol
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-2-final vet -mod=readonly ./internal/protocol
```

- Test: PASS, 1.047s (`task-2-test.log`), including the new error-sequence regression.
- Race: PASS, 5.225s (`task-2-race.log`).
- Vet: exit 0, no output (`task-2-vet.log`).
- `git diff --check -- patches/0008-relaykit-module.patch patches/README.md`: exit 0.
- Additional full-core test invocation: `.build/dev-tools/go/bin/go.exe -C .build/protocol-check-2-final test -mod=readonly ./...`; **PASS, all 22 core packages**, including existing Anthropic, bridge, scheduler, taskrun and server packages (`task-2-core-tests.log`).

## Self-review and handoff

Reviewed the request callers (none wired yet), library public registry signatures, actual event DTOs, aggregate diagnostics behavior, nullable count handling, synchronous stream/error/cancel lifecycle and module overlay patch. The implementation deliberately uses library field/event mapping instead of another converter. No new runtime dependencies beyond the pinned library's existing graph.

- Task 3 must extend `validateResponses`' input-type switch **together with** the library history patch to allow validated plain `reasoning.summary` and continue rejecting encrypted/opaque content. Task 2 rejects reasoning history explicitly to avoid the unpatched library silently treating it as an empty user message. This temporary boundary is confirmed by the controller; it does not remove the final history requirement.
- Task 4 must replace the current Claude text-only request allowlist/shape checks with enhanced validation. Mixed Claude output transport is already covered; request tools/thinking/media are intentionally not exposed by Task 2.
- Route/method/auth/version enforcement belongs to the caller as specified. There is no Responses/Anthropic public wiring in this commit.
- SDK, full project shell checks and isolated deployment acceptance are owned by subsequent integration tasks. This task does not claim these or actual upstream capability verification.
- Preserved the preexisting `.gitignore`, untracked `upstream-convert/` and model/protocol probe data, as well as controller-owned concurrent docs/environment edits. No real credentials/data, service restarts, publishing, pushing, or deployment were used.
