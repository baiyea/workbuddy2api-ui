# Real WorkBuddy wire compatibility correction

Date: 2026-10-11. Scope: adapter-only interpretation of observed inert WorkBuddy SSE placeholders. The original Chat Handler, upstream snapshot, RelayKit snapshot, patches, credentials, services and model-probe scripts are unchanged by this correction.

## Evidence and cause

The parent task observed six failed real text probes (three models, both adapters), while the same account's legacy Chat request succeeded. Its trusted raw callback captured text and tool streams. This implementation used their JSON shapes only; maintained fixtures use synthetic IDs, timestamps, content, tool arguments and usage. It made no real upstream calls and did not read credential contents.

WorkBuddy emits `function_call:null`, `extra_fields:null`, `refusal:""`, empty `tool_calls`, and `finish_reason:""` on nonterminal chunks. Its final delta can contain `function_call:{"name":"","arguments":""}`. The adapter's strict delta DTO rejected the unsupported placeholder fields, and its finish validation rejected the empty nonterminal finish. Tool continuations have `function:{"name":"","arguments":"fragment"}`; existing accumulation already handles that shape correctly.

## Minimal correction and boundaries

`extensions/internal/protocol/stream.go` removes only the observed inert `function_call` null/exact empty object, `extra_fields` null and empty-string `refusal` before strict delta validation. Every other field remains subject to the same strict validation. An empty finish means no finish; only an existing valid, nonempty finish can enable success. Original raw byte accounting, frame limits, cancellation, DONE ordering and end validation remain in place. No repeated-name rewriting was introduced.

Full original-Handler regressions cover both Responses and Messages, JSON and SSE: text plus truthful usage; tool ID/name/fragmented object arguments; feeding the generated tool output back with its result and inspecting actual outbound Chat history. Negative cases retain rejection of nonempty legacy function fields, extra function fields, refusal text, unknown fields, non-null extra fields (including `{}`), missing terminal finish, whitespace finish and unknown finish. Existing malformed/raw-size/cancellation/terminal and strict tool suites also run in the complete affected package checks.

## RED / GREEN verification

Native toolchain: `.build/dev-tools/go/bin/go.exe`, Go 1.27.2 windows/amd64. All materializations came from `scripts/overlay.py prepare` into fresh output directories; the frozen upstream was not edited.

| Check | Result / evidence |
| --- | --- |
| Pre-fix full Handler text fixture, both formats and modes | All four positive cases failed with 502; negative cases passed. `real-wire-red.log` |
| Pre-fix full Handler tool fixture, both formats and modes | All four cases failed with 502. `real-wire-tools-red.log` |
| Corrected text and negative regression cases | PASS. `real-wire-green1.log` |
| `go -C .build/protocol-real-wire-green2 test ./internal/server ./internal/protocol ./internal/anthropic ./internal/responses ./internal/upstream -count=1` | All five packages PASS. `real-wire-green2.log` |
| `go -C .build/protocol-real-wire-green2 vet ./internal/server ./internal/protocol ./internal/anthropic ./internal/responses ./internal/upstream` | Exit 0. `real-wire-vet.log`, `real-wire-exits.log` |
| `go -C .build/protocol-real-wire-green2 test -race ./internal/server ./internal/protocol ./internal/anthropic ./internal/responses ./internal/upstream` | All five packages PASS, exit 0. `real-wire-race.log`, `real-wire-exits.log` |

Logs are local verification artifacts beside this report. The self-review checked that normalization cannot turn an empty finish into successful completion, accepts no unobserved empty unknown fields, and leaves raw transport checks and original Chat behavior untouched.

## Remaining integration ownership

Independent review, exact-source image rebuild and the authorized real three-model feature matrix belong to the parent task and are not claimed by this report. These native synthetic regressions establish the wire correction, not model feature support or real-call success. No publish, push, deployment or actual-service restart was performed. Unrelated workspace changes and real probe evidence were preserved.
