# Task 4 review

## Spec Compliance

- ❌ Issues found. The task covers every listed implementation file and the requested history patch, but three boundary cases violate the accepted no-loss/actual-output requirements: empty reasoning permits absent output (`extensions/internal/protocol/adapter.go:235`), accepted `thinking.display` disappears across serialization (`extensions/internal/protocol/claude_validation.go:110`), and invalid Claude tool arguments become invented `{}` (`extensions/internal/protocol/adapter.go:242`).
- ✅ The controller ruling for `tool_choice` is implemented: it is excluded from the accepted top-level fields and has a path-specific rejection test (`extensions/internal/protocol/claude_validation.go:17`, `extensions/internal/protocol/claude_validation_test.go:20`). No second conversion chain was added.
- ⚠️ Cannot verify from this task diff: cross-task frozen-source provenance and packaging, bridge-only initialization, complete console/owner/CSRF wiring, SDK parsing, container acceptance, and final README capability descriptions. Task 4's report explicitly delegates SDK/container integration and documentation to Task 5 (`docs/superpowers/implementation/protocol-conversion/task-4-report.md:59`). The controller should verify these in the integration gate; their absence here is not a Task 4 scope defect.

## Strengths

- Authentication/header/query/compression checks remain at the Anthropic entry, and the trusted conversation context is passed directly to shared in-process transport (`extensions/internal/anthropic/adapter.go:40`, `extensions/internal/anthropic/adapter.go:61`). Deleting the replaced text converter and stream writer avoids maintaining parallel transports.
- Nested validation uses exact key membership and rejects external signed/opaque reasoning before conversion (`extensions/internal/protocol/claude_validation.go:154`, `extensions/internal/protocol/claude_validation.go:226`). Tools, text and image input have real conversion assertions, rather than only status checks (`extensions/internal/anthropic/conversion_test.go:32`, `extensions/internal/anthropic/conversion_test.go:67`).
- The small library patch preserves mixed assistant text/tool calls and thinking-only history, and carries its own independent regression covering call/result association (`patches/0010-relaykit-claude-history.patch:19`, `patches/0010-relaykit-claude-history.patch:24`, `patches/0010-relaykit-claude-history.patch:36`). Its removal condition is documented (`patches/README.md:156`).
- Existing rejection tests were changed only where expanded capabilities require it; dedicated tests preserve shared legacy/refusal errors and requested Claude model identity (`extensions/internal/protocol/completion_validation_test.go:13`, `extensions/internal/protocol/completion_validation_test.go:26`). The mixed thinking/text/tool SSE regression explicitly checks late zero usage and final stop reason (`extensions/internal/anthropic/conversion_test.go:99`).

## Issues

### Critical (Must Fix)

None found in the reviewed scope.

### Important (Should Fix)

1. **[P2] Empty reasoning bypasses missing-output rejection — `extensions/internal/protocol/adapter.go:235`.** With absent or null `content`, no tool calls, and `reasoning_content:""`, unmarshalling creates a non-nil string pointer, so validation succeeds. Neither response mapper treats an empty string as actual reasoning: Claude synthesizes an empty text block and an `end_turn`; Responses can emit `completed` with empty output. This violates the controller-approved rule that absent/null content is accepted only with actual reasoning or tools. Require nonempty reasoning for this exception (`reasoning == nil || *reasoning == ""`); preserve explicitly present `content:""`. Add absent/null-content × empty-reasoning regressions for both formats and positive coverage for real reasoning-only/tool-only responses. Confirmed from the validation branch and target mapper code; no test rerun was necessary.

2. **[P2] `thinking.display:"omitted"` is accepted but silently ignored — `extensions/internal/protocol/claude_validation.go:110`.** The pinned mapper places the visibility intent in `ReasoningConversion.IncludeThoughts`; that DTO member is `json:"-"`. Shared `Serve` marshals the Chat request, so the visibility control never reaches the Handler. Claude response conversion emits any returned reasoning unconditionally, including for an accepted omitted request. The supported-input report therefore promises a typed control with no effect. Reject this unrepresentable field with the `thinking.display` path before invoking the Handler, unless its semantics are explicitly implemented within the approved transport contract; avoid adding a second mapper. Add an entry regression proving the request fails before the Handler, and update the Task 5 documentation handoff. The native numeric thinking budget also becomes effort rather than an exact serialized token budget; document that existing approximation without promising exact budget enforcement.

3. **[P2] Malformed Claude tool arguments are replaced with `{}` — `extensions/internal/protocol/adapter.go:242`.** The completion validator checks only that the argument string exists. The adopted Claude response mapper initializes an empty object and keeps it whenever parsing fails or the JSON is not an object (`extensions/third_party/relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:483`). An ordinary successful tool completion with `arguments:"not-json"`, `"[]"` or `"null"` therefore returns a successful `tool_use` with fabricated empty input; the registered converter supplies no loss diagnostic. Reject unrepresentable non-object/malformed final arguments before Claude conversion with the existing sanitized 502 behavior. Add ordinary Claude entry coverage for these cases, valid `{}`, and a nonempty valid object; preserve Responses behavior when applying the guard. This finding confirms the ordinary JSON path only; the stream argument-completion path was not inspected and is not asserted defective here.

### Minor (Nice to Have)

None beyond the required documentation clarification in issue 2.

## Checks and scope

- Read the complete 1,836-line supplied review package once, in bounded sections because the initial combined tool output was truncated. Reviewed base `1a6ae63ef3f3337037b2db064f2b602a99f66e46` to head `e7630fa5b3c92349eacef2a7f0a418d430f97b8c`. No code/index/branch changes or real service/upstream calls were made.
- Named risk: moving the entry to shared transport could bypass previous request/body/context validation. Checked the relevant shared `Serve` and `parseRequest` sections outside the diff; retained limits, per-request metadata, trusted conversation injection and raw request boolean checks were visible. The changed adapter diff cuts these functions, so the focused context read was necessary.
- Named risk: newly accepted native thinking/media/tool-result shapes could be lost by the pinned direct mapper. Checked `claude_messages/to_oai_chat_req.go`, the relevant `reasoning/intent.go` intent/Chat application sections and the `dto/openai_request.go` serialization member. This confirmed issue 2 and did not reveal a new history-patch defect.
- Named risk: completion output-presence and tool-argument checks may diverge from what the target mappers emit. Checked the ordinary Claude/Responses response mapper sections and the public Claude response registration; confirmed issues 1 and 3. Also read the pinned library's existing malformed-tool-argument fixture, which deliberately normalizes these cases and therefore does not protect the host no-repair contract.
- Inspected the saved entry/library RED logs, core/library final test logs, both race logs and both vet logs. RED failures demonstrate the requested behavioral gaps; final test/race logs show passing processes, and vet logs contain no diagnostics. Did not rerun passed suites, race or vet. No focused execution was needed because the three findings follow directly from the exact branches and mapper behavior.

## Assessment

**Task quality: Needs fixes.** The transport consolidation and minimal history patch are sound, with useful regressions and visible verification evidence. The three confirmed boundary gaps must be corrected before the Task 4 gate is approved.

Skipped suite reruns and cross-task SDK/container/real-upstream validation; those remain integration checks. The confirmed risks are false successful output, ignored thinking visibility, and fabricated Claude tool input.
