# Task 3 — Responses creation and plaintext history

Base: `8954bef4a8c1ee464c8d99ca657dd8fa914fbef7`. Work stayed in the shared
checkout; no branches, worktrees, or subagents were created. Only Task 3 files
were staged. Existing `.gitignore`, source probes, `upstream-convert/`, and
controller-owned docs remain untouched by this task.

## Delivered

- `responses.New(next, apiKey, maxBodyBytes)` intercepts only `/v1/responses`.
  POST uses one exact Authorization Bearer header, constant-time key comparison,
  the existing request-size limit, and the same query/compressed-body rejection
  rules. GET returns 405/Allow POST; duplicate/missing/incorrect credentials
  return 401. Other paths pass the original request to `next`.
- `wrapCore` installs the Responses wrapper only after the bridge-key check,
  alongside the existing Anthropic wrapper. Source mode keeps the original
  Handler. Calls stay in-process with the same pool and Handler; forged private
  request headers cannot become a conversation ID.
- The adapter delegates mapping, usage, cancellation and stream transport to
  Task 2's reviewed `protocol.Serve`/`WriteError`. No second mapping layer,
  response store, Files API, HTTP loopback, or background tool executor was added.
- `0009-relaykit-responses-history.patch` maps plaintext `summary_text` blocks
  into Chat `reasoning_content`, preserving following assistant text and tool
  calls in the same turn. It handles text before tools and tools before text.
  Three library regressions are carried inside the patch. Original upstream and
  RelayKit snapshot files and SHA manifests were not edited; 0008 stays in place.
- The shared Responses validator now accepts a validated plaintext summary,
  including an empty array or empty text. Unknown reasoning fields, encrypted
  content (even null), opaque content, signatures, malformed summaries, and
  unknown summary-block fields return 400 with an `input[i]...` path before next.
- `TestGeneratedPlaintextHistoryRoundtrip` directly takes the first response's
  original `output` array, adds the tool result and next user input, then posts
  it again. It checks one assistant turn keeps reasoning, text, name/arguments,
  call_id and the linked tool result; it does not clean up the generated object.

The fixed RelayKit `ResponsesOutput` serializer emits `content:null`, `role:""`,
`quality:""`, and `size:""` on reasoning items. Only these exact inert values are
additionally accepted by the validator and library patch, so actual generated
objects can roundtrip. Non-null content, nonempty inert string fields, and all
other unknown fields are rejected. This exception is not a general unknown-field
ignore rule.

## RED evidence

Commands below were run using fresh overlay paths. All logs stay next to this
report (the directory's existing `.gitignore` intentionally leaves logs local).

```powershell
python scripts/overlay.py prepare --output .build/protocol-task3-route-red
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-route-red test -mod=readonly ./cmd/server -run TestCoreResponsesUsesSharedHandlerOnlyWithBridge
```

Exit 1: the bridge branch still sent `/v1/responses`, input and forged headers to
the raw Handler, and returned a Chat object. Source mode passed unchanged.
Log: `task-3-route-red.log`.

```powershell
python scripts/overlay.py prepare --output .build/protocol-task3-history-red2
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-history-red2 test -mod=readonly ./internal/responses ./cmd/server -run 'TestRoute|TestCoreResponses|TestToolHistory|TestConcurrentCustom|TestGeneratedPlaintext|TestUnsupportedState|TestStreamAggregates'
```

Exit 1: the generated history's first reasoning item was rejected before next
with `input[0].type`. Route, tools, concurrent custom tools, state rejection,
stream sequence/aggregation and wiring tests already passed using Task 2's
shared implementation; no replacement tool/event conversion was needed.
Log: `task-3-history-red2.log`. An earlier build attempt caught an untyped
format constant being inferred as string; it was corrected before this behavior
RED run (`task-3-history-red.log`).

Library history tests were added to that materialization before changing its
mapper; the original library source was still unchanged for the RED run:

```powershell
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-history-red2/third_party/relaykit test -mod=readonly ./relayconvert/internal/oai_responses -run 'TestResponsesPlaintextReasoningHistory|TestResponsesReasoningWithoutText'
python scripts/overlay.py prepare --output .build/protocol-task3-validation-red
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-validation-red test -mod=readonly ./internal/protocol -run 'TestResponsesPlaintextReasoningValidation|TestResponsesOpaqueAndMalformedReasoning'
```

Both exit 1. Library output contained an empty user message instead of reasoning,
splitting assistant history. The validator rejected valid summaries and lacked
specific malformed/opaque field paths. Logs: `task-3-library-red.log`,
`task-3-validation-red.log`.

Self-review then added the tool-before-text ordering regression to the working
materialization before removing the unnecessary empty-tool-list merge condition:

```powershell
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-history-red2/third_party/relaykit test -mod=readonly ./relayconvert/internal/oai_responses -run TestResponsesReasoningToolThenTextSharesAssistant
```

Exit 1: reasoning/tools and text became two assistant messages. Log:
`task-3-order-red.log`. The final patch carries the passing regression.

Intermediate green attempts also exposed patch CRLF bytes under Windows
(`task-3-core-green1.log`) and the serializer's inert reasoning fields
(`task-3-core-green2.log`). The patch is now written as LF bytes and fresh
materialization applies it normally; the original-object regression now passes.

## Final GREEN evidence

Native toolchain: `go version go1.27.2 windows/amd64` from the controller-provided
`.build/dev-tools/go/bin/go.exe`; gcc is on PATH for race tests. These results
do not substitute for the controller's Docker Go 1.26 build baseline.

```powershell
python scripts/overlay.py prepare --output .build/protocol-check-3-final2
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-3-final2 test -mod=readonly ./internal/responses ./internal/protocol ./internal/server ./cmd/server
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-3-final2 test -mod=readonly -race ./internal/responses ./internal/protocol ./internal/server ./cmd/server
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-3-final2/third_party/relaykit test -mod=readonly ./...
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-3-final2/third_party/relaykit vet -mod=readonly ./...
.build/dev-tools/go/bin/go.exe -C .build/protocol-check-3-final2 vet -mod=readonly ./internal/responses ./internal/protocol ./internal/server ./cmd/server
git diff --check -- extensions patches
```

All exit 0. Core PASS: Responses 0.872s, protocol 1.018s, server 31.560s,
cmd/server 1.832s. Race PASS: Responses 6.845s, protocol 5.667s, server 34.418s,
cmd/server 4.011s. The full nested library module passed all tested packages.
Logs: `task-3-core-final.log`, `task-3-race-final.log`,
`task-3-library-final.log`. Both vet commands returned exit 0 without output;
PowerShell's Tee-Object did not create the named empty vet log files. These vet
results are recorded by the tool outputs, not by nonexistent log artifacts.

The new endpoint regressions cover model/output-limit mapping, text/image/schema
and strict function tools, parallel call/result ID ordering, concurrent request
metadata isolation for custom and function calls, ordinary completed/incomplete
status, strict single-header auth, body/query/compression limits, stateless false
flags and state rejection, immediate streaming, one delayed terminal, continuous
sequence numbers, parallel argument aggregation, reasoning/text deltas and exact
output-item.done/final-output equality. Existing shared transport tests cover
late/unknown/zero usage, bounded streams, cancellation, malformed/error terminals
and writing/flush failures.

## Self-review and handoff

- Scope checked across the wrapper's sole wiring caller, shared validation,
  existing rejection fixture, patch series/maintenance notes, and new endpoint,
  wiring and library tests. No unrelated production/config changes.
- Authentication precedes reading the body. Rebuilt Chat headers are the shared
  layer's minimum headers. Context and cancellation remain the shared layer's
  responsibility; no API/bridge/admin permission mixing is introduced.
- Library argument generation and function/custom metadata restoration remain
  untouched; schema outputs are neither repaired nor validated against a schema.
- `patches/README.md` records validation and removal conditions. Materializations
  are disposable and source modifications have been returned to extensions or
  the patch, never left only in `.build`.
- Task 4 may now accept plaintext Responses reasoning through the shared layer;
  its Anthropic history patch should be a subsequent patch, leaving 0009 and
  Task 3 public wiring in place.

SDK checks, full project shell/console/Python checks and isolated Docker
acceptance are owned by the controller's integration work and were not duplicated
here. No actual upstream calls, real credentials/data, service restart,
publication, Git push, or deployment occurred. Mock Handler tests establish
conversion contracts only, not real model capability or SDK/container acceptance.

## Review round 1 — tool-first streaming history

Fix base: `5602eb2b9a5ce53de28dc062c494f86ec41fb39e`. Technically verified I1 from
`task-3-review.md`: stream output preserves first appearance, so a tool can
precede reasoning and text in a valid final response. The original 0009 reasoning
branch appended another assistant, splitting a generated call/result batch.
Text arriving between tools and reasoning also created a separate assistant.

Before changing the mapper, added
`TestGeneratedStreamHistoryKeepsInterleavedAssistantTurn`. Six real SSE orderings
exercise all relative text/reasoning/tool orders, including calls interleaved on
either side of reasoning. The regression confirms emitted output retains the
first-seen order, resubmits those original output objects without modifying
order or serializer fields, adds two tool results, and requires one assistant
with both names/arguments/IDs, reasoning and text followed by its linked results.
Five orderings failed with split assistant messages; the prior reasoning-first
ordering passed. An independent library regression also reproduced call →
reasoning → text → result splitting, and covers call → text → reasoning and
text → call → reasoning.

RED commands:

```powershell
python scripts/overlay.py prepare --output .build/protocol-task3-review1-red
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-review1-red test -mod=readonly ./internal/responses -run TestGeneratedStreamHistoryKeepsInterleavedAssistantTurn
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-review1-red/third_party/relaykit test -mod=readonly ./relayconvert/internal/oai_responses -run TestResponsesToolFirstReasoningKeepsAssistantBatch
```

Both exit 1 with the expected split-message failures. The library regression was
added in that materialization before changing its source, then carried back in
0009. Logs: `task-3-review1-endpoint-red.log`, `task-3-review1-library-red.log`.

The minimal mapper fix attaches a reasoning item to the preceding contiguous
assistant if present, retaining its existing text/tool calls and reasoning.
Assistant text with no preceding content also joins an already-started
reasoning/tool assistant. User/tool messages remain boundaries. No SSE output
order or index is changed, and the shared transport/validator is unchanged.
The patched library regression and maintenance notes accompany the change.

GREEN commands:

```powershell
python scripts/overlay.py prepare --output .build/protocol-task3-review1-final
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-review1-final test -mod=readonly ./internal/responses ./internal/protocol ./cmd/server
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-review1-final/third_party/relaykit test -mod=readonly ./relayconvert/internal/oai_responses ./relayconvert
.build/dev-tools/go/bin/go.exe -C .build/protocol-task3-review1-final test -mod=readonly -race ./internal/responses ./internal/protocol
git diff --check -- extensions/internal/responses patches docs/superpowers/implementation/protocol-conversion/task-3-report.md
```

All exit 0. Core PASS: Responses 0.870s, protocol 0.299s, cmd/server 0.489s.
Focused library PASS: oai_responses 0.087s, public relayconvert 0.128s.
Race PASS: Responses 2.528s, protocol 5.283s. Logs:
`task-3-review1-core-final.log`, `task-3-review1-library-final.log`,
`task-3-review1-race-final.log`. All six own-output ordering cases now pass.

Self-review checked both changed merge branches against the function-call helper
and all regression permutations. The call/result batch remains contiguous,
existing text/tool state is retained, and no user/tool boundary is crossed.
No changes were needed in transport, validation, public wiring or dependencies.
Broad vet is not repeated for this focused fix; Task 5 owns fresh full-core/library
production-toolchain checks. The earlier incorrect references to empty vet log
files were corrected above. No new service, state, routing, authorization,
snapshot or deployment changes.
