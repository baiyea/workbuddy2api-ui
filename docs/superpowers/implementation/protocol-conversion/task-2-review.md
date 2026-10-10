# Task 2 review — protocol_review2

Range: 728f5a0784af77b58b2cb134c3fbd8682fd5d791..5b95153e91d87300d6b2d91b8b3647798fc00a6a.

Spec compliance: issues found. Task quality: needs fixes.

## Important: invalid tool results can become successful empty output

`extensions/internal/protocol/adapter.go:209` only checks that tool_calls exists, not that it is a valid array. This upstream response passes host validation:

```json
{"id":"c","model":"test","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":{}},"finish_reason":"tool_calls"}]}
```

RelayKit `dto.Message.ParseToolCalls()` swallows decoding errors and the Responses converter emits completed with empty output. An array entry without a tool name is also silently skipped.

Streaming has the same gap at `stream.go:148` and `stream.go:283`: DTO decoding does not guarantee final tool completeness. A stream containing an ID/arguments but never a name is buffered then dropped by the library, yet response.completed may still be sent.

Fix ordinary tool structure validation and validate accumulated stream tool fields before synthesizing success finish. Invalid results must cancel and produce sanitized errors without a success terminal. Add JSON-object tool_calls, missing-name array entries, and unfinished streaming tool regressions. This does not require validating or repairing generated arguments against JSON Schema.

Focused library evidence: dto/openai_request.go:657 discards parse errors; relayconvert/internal/oai_chat/to_oai_responses_resp.go:109 drops unnamed tools; to_oai_responses_stream_resp.go:518 holds unnamed tools and drops them on completion; internal/toolconv/response.go:17 lacks an OpenAI Chat diagnostics branch. Existing to_oai_responses_resp_test.go:46 expects missing names to be dropped.

## Other conclusions

No Critical or Minor findings. Positive evidence: per-request metadata and custom-tool isolation; preserved context with rebuilt minimal headers; nullable usage presence; dual stream bounds, immediate output, truncated streams and writer cancellation tests. Reviewer read final protocol test/race/vet logs; did not rerun suites or mutate checkout.

Cross-task checks remain with controller: public auth/headers/bridge-only wiring, actual owner/envelope path, Task3 reasoning history, Task4 enhanced Claude input, and Task1 provenance. These are already assigned to later tasks or verified Task1, not an additional Task2 defect.
