# Task 4 — Anthropic conversion and plaintext history

Status: DONE. Base: `1a6ae63ef3f3337037b2db064f2b602a99f66e46`.

## Implementation

- Anthropic keeps `New`, `WithConversation`, x-api-key authentication, exact version checking, beta/query/compression rejection and pass-through behavior. After these checks it calls approved `protocol.Serve` with the trusted context conversation ID. Bridge/envelope/owner callers are unchanged.
- Deleted the obsolete text converter and separate `anthropic/stream.go`; shared bounded in-process transport now handles tools, images, generation parameters, unsigned thinking, JSON and SSE. There is no HTTP loop, second pool or tool execution.
- Added `validateClaudeRequest(raw []byte, input *dto.ClaudeRequest) error` for required presence, nested shape, unknown fields, roles, tools, sources and opaque reasoning. Request/response mapping remains public RelayKit conversion with the existing safe diagnostics policy.
- Added `0010-relaykit-claude-history.patch`: preserve assistant media/text beside tool calls, collect plaintext thinking into `reasoning_content`, retain thinking-only turns. Patch contains its own independent library regression. Original `extensions/third_party/relaykit`, `upstream`, `upstream-convert` and source manifest bytes were not edited. Fresh materialization passed frozen-source checks and patch application.
- Existing assertions now accept RelayKit text part arrays, supported thinking and content_filter → refusal stop mapping. Authentication, request isolation, cancellation, trusted conversation limits, malformed input, error sanitization, output bounds and unknown/late/zero usage regressions remain.
- Controller-approved shared change in `protocol/adapter.go` retains old errors for nonempty legacy function calls/refusals, validates reasoning strings and rejects absent/null output unless there is actual reasoning or tools. Both formats have rejection coverage. Claude ordinary responses keep the requested model; Responses normalization remains unchanged.

## Supported and explicitly rejected input (Task 5 documentation handoff)

Top-level accepted keys: `model`, `max_tokens`, `messages`, `system`, `stream`, `temperature`, `top_p`, `top_k`, `stop_sequences`, `tools`, `thinking`, `output_config`. Required model/max_tokens/messages and primitive shapes remain checked. `output_config` accepts only `effort`; `thinking` accepts `type`, positive `budget_tokens`, and supported `display`. The library validates reasoning intent/effort combinations. Tools require nonempty name and object input_schema; descriptions may be empty. `tools:[]`, empty stop list, empty text strings, explicit stream:false, signature:"" and tool_result is_error:false are accepted. Null substitutes for these required/typed values are rejected.

All other top-level keys return 400 by field path, including `tool_choice`, `prompt`, `max_tokens_to_sample`, `cache_control`, `inference_geo`, `safeguards`, `context_management`, `output_format`, `container`, `mcp_servers`, `metadata`, `chat_template_kwargs`, `speed`, `service_tier`, and private `conversationId`. These fields are not carried by the pinned direct Claude → Chat mapper. Do not describe forced tool choice or schema output as supported by this Anthropic route.

- Tool definitions accept `name`, `description`, `input_schema`. `tools[i].strict` (including false/null), tool `type`/server fields, cache metadata or any other unknown key are rejected; this mapper does not transfer them. The schema object is preserved without implementing JSON Schema validation/repair.
- Messages accept only `role`, `content`, with user/assistant roles. System accepts text only. Text blocks accept `type`, `text`; cache/citations/unknown keys fail at `system[i].key` or `messages[i].content[j].key`.
- Thinking history accepts assistant `type:"thinking"`, string `thinking`, and absent/empty-string signature. Nonempty or malformed `messages[i].content[j].signature` returns 400. `redacted_thinking` and other opaque blocks fail at `.type`. No signature is generated.
- Tool use requires assistant block ID/name and object input. Tool results require user block tool_use_id and text or content blocks. `is_error:true`/null/nonboolean return 400 at `messages[i].content[j].is_error`; absent/false is accepted because the mapper cannot carry the error flag. Unknown name/server metadata are rejected rather than silently lost.
- Images accept HTTP(S) URL or strict nonempty base64 with PNG/JPEG/GIF/WebP MIME. Documents accept text/plain text or base64 application/pdf. Invalid sources, file/file_id, document URLs and unsupported MIME/types are rejected at `.source` and child paths. Blocks accept only `type`/`source`; extra document context/title/cache/citation fields are rejected. No Files API resolution is added.

## TDD evidence

Native Go executable for every command: `.build/dev-tools/go/bin/go.exe`; dependency mode `-mod=readonly`; Python `python scripts/overlay.py prepare --output PATH` used a new nonexistent path each time.

1. Entry RED in `.build/protocol-check-4-red`: `go -C PATH test -mod=readonly ./internal/anthropic -run 'Test(ToolDefinitionsCallsAndResults|ImagesAndGenerationParameters|ThinkingOutput|ToolStreamKeepsLateZeroUsage)$' -v`. All four failed for expected old behavior: tools/parameters/thinking rejected 400, mixed tool/thinking stream rejected 502. Evidence: `task-4-entry-red.log`.
2. Original library RED (0010 not yet present) in the same overlay: `go -C PATH/third_party/relaykit test -mod=readonly ./relayconvert/internal/claude_messages -run '^TestClaudePlaintextMixedHistory$' -v`. Thinking-only messages disappeared; mixed text/tool assistant content had zero parts. The test was corrected for the DTO accessor before capturing the actual behavioral failures. Evidence: `task-4-library-red.log`.
3. Nested validator RED in `.build/protocol-check-4-validation-red`: `go -C PATH test -mod=readonly ./internal/protocol -run '^TestClaudeNestedValidation$' -v`. Old validator rejected newly supported shapes and lacked required nested paths. Evidence: `task-4-validation-red.log`.
4. Entry and validator GREEN in `.build/protocol-check-4-green-2`: `go -C PATH test -mod=readonly ./internal/anthropic ./internal/protocol -run 'Test(ToolDefinitionsCallsAndResults|ImagesAndGenerationParameters|ThinkingOutput|ToolStreamKeepsLateZeroUsage|ClaudeNestedValidation)$' -v`. All passed. Evidence: `task-4-focused-green.log`.
5. Retained output errors and requested model RED in `.build/protocol-check-4-boundary-red`: `go -C PATH test -mod=readonly ./internal/protocol -run 'Test(CompletionRetainsUnsupportedOutputErrors|ClaudeResponsePreservesRequestedModel)$' -v`. Shared layer returned 200 for nonempty legacy/refusal and null absent output in both formats; Claude used upstream model alias. Evidence: `task-4-boundary-red.log`. GREEN with adapted existing contracts in `.build/protocol-check-4-iteration-3`, run `go -C PATH test -mod=readonly ./internal/anthropic ./internal/protocol -run 'Test(Text|Reject|ResponseMapping|RealAggregate|Stream|CompletionRetainsUnsupportedOutputErrors|ClaudeResponsePreservesRequestedModel)' -v`. All passed: `task-4-contract-green.log`.
6. Self-review RED in `.build/protocol-check-4-field-red`: `go -C PATH test -mod=readonly ./internal/protocol -run 'TestClaudeNestedValidation/model_max_tokens' -v`. An unknown key containing a sequence of allowed names was initially accepted by substring matching. Changed to exact field membership; final full protocol tests passed. Evidence: `task-4-field-red.log` and `task-4-core-final.log`.

## Final verification

Fresh final materialization: `.build/protocol-check-4`. Every following process exited 0:

| Command from repository root (using native Go path) | Result / evidence |
| --- | --- |
| `go -C .build/protocol-check-4/third_party/relaykit test -mod=readonly ./...` | 9 tested library packages passed, `task-4-library-final.log` |
| `go -C .build/protocol-check-4/third_party/relaykit vet -mod=readonly ./...` | no diagnostics, `task-4-library-vet.log` |
| `go -C .build/protocol-check-4 test -mod=readonly ./internal/anthropic ./internal/protocol ./internal/bridge ./cmd/server` | all four passed, `task-4-core-final.log` |
| `go -C .build/protocol-check-4 vet -mod=readonly ./internal/anthropic ./internal/protocol ./internal/bridge ./cmd/server` | no diagnostics, `task-4-core-vet.log` |
| `go -C .build/protocol-check-4 test -mod=readonly -race ./internal/anthropic ./internal/protocol ./internal/bridge ./cmd/server` with CGO_ENABLED=1 / installed GCC | all four passed, `task-4-core-race.log` |
| `go -C .build/protocol-check-4/third_party/relaykit test -mod=readonly -race ./relayconvert/internal/claude_messages` | passed, `task-4-library-race.log` |
| `git diff --check` scoped to own paths | passed; only local checkout LF/CRLF notices |

Intermediate patch generation initially used CRLF and failed git apply; normalized patch bytes to LF before any successful materialization. No dependency or frozen source bypass was used.

## Files and self-review

Changed: `extensions/internal/anthropic/adapter.go`, `adapter_test.go`, `stream_test.go`; deleted `stream.go`; added `conversion_test.go`; shared `extensions/internal/protocol/adapter.go`; added `claude_validation.go`, `claude_validation_test.go`, `completion_validation_test.go`; `patches/0010-relaykit-claude-history.patch`, `patches/series`, `patches/README.md`; this report. Logs are ignored and nearby.

Self-review checked callers, trusted owner/conversation context, prior completion errors, source boundaries and patch scope. No unresolved implementation concern. Explicit conversion limitations above require Task 5 README coverage.

No SDK/container verification, real upstream call, push, image release, deployment or actual service restart was performed; Task 5 owns integration. Preserved unrelated `.gitignore`, progress/Task 5 brief edits, untracked deferred/probe reports and `upstream-convert/`.
