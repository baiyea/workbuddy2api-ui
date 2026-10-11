# Final whole-change review

Range: `8cbf7ae9943e85bda92f81778b056435479fa954` → `e213c4b8db3bec7f70a178cf9e83fa62fe1be5ff`. Reviewed against the approved protocol spec/plan, current AGENTS, controller rulings, owned implementation and the relevant pinned RelayKit/original Handler paths. **Integration verdict: With fixes; not ready to merge.** Seven Important findings below are reproduced through the real Handler, rather than inferred from task reports.

## Strengths

- The architecture follows the plan: two services, bridge-only wrappers, the existing Handler/account pool, request-local conversion metadata, and no HTTP loopback or production SDK dependency. Public API authentication, bridge authentication, trusted conversation context, management owner/CSRF and cancellation remain distinct.
- Original third-party bytes are preserved and fixes use overlays. I independently compared all 162 vendored files to both SOURCE.json hashes and Git blobs at pinned New API commit `0f38a14a5e8e322ea741478b0e49878be3130e1d`: zero mismatches. Frozen upstream/upstream.lock and production Compose have no diff from the baseline. Notices are included in Docker and desktop resource preparation; attribution is visible.
- The request/body boundary, request-scoped custom tools, nullable usage observer, delayed stream finalization, plaintext history patches, and SDK text-part lifecycle patch are well focused. The central weakness is that several guarantees begin *after* the original Handler has already transformed or buffered the raw upstream stream.
- Saved evidence genuinely establishes Linux Go 1.26.9 core/library test/vet/race, console/Node/Python, pinned SDKs and isolated container acceptance. I checked the successful gateway log and image identities: core `37f940775ebb…`, console `db86d5f48d66…`, linux/amd64. The earlier acceptance exit 1 is retained separately from final exit 0. None of this is evidence of real-model capability.

## Issues

### Critical (Must Fix)

None classified Critical. The following are concrete correctness/resource-bound failures and must be resolved for the approved integration contract.

### Important (Should Fix)

1. **[P1] The real Handler destroys termination evidence before the new validator sees it.**
   - Integration references: `extensions/internal/protocol/adapter.go:94`, `:99`, `:106`; `extensions/internal/protocol/stream.go:320`. Underlying behavior: `upstream/internal/upstream/sse.go:26`, `:43`, `:396`, `:402`; `upstream/internal/server/handler.go:631`, `:650`.
   - Ordinary requests go through `Aggregate`, which defaults finish_reason to `stop`, skips malformed JSON frames and stops at DONE. Stream requests go through `Stream`, which stops at DONE; the Handler discards its returned read error. A normally returning Handler therefore does not prove a normally completed upstream stream.
   - Executed `TestFinalReviewRealTerminal`: both formats falsely succeed for ordinary text + DONE without any finish_reason, malformed data after a finish, streamed data after DONE, and `io.ErrUnexpectedEOF` after a streamed finish. Responses emits `completed`; Messages emits `end_turn`/`message_stop`.
   - Add a trusted adapter-only strict transport contract before the original aggregation/normalization, preserving read errors, frame boundaries and terminal evidence. Apply changes to original files through patches; preserve ordinary OpenAI/no-bridge behavior. Regress these at the real Handler boundary, including errors/truncation after finish, rather than only a fake next Handler.

2. **[P1] Raw upstream response limits are enforced after unbounded buffering.**
   - References: `extensions/internal/protocol/adapter.go:99`, `:155`; `extensions/internal/protocol/stream.go:107`; underlying `upstream/internal/upstream/sse.go:34`, `:396`.
   - Ordinary `Aggregate` reads/builds the entire upstream response before the 32 MiB responseBuffer can reject its serialized result. Both upstream paths use unbounded `ReadString` before the new writer receives a frame. A large unterminated line can exhaust memory before the 8 MiB guard; an indefinitely growing ordinary stream can bypass all writer bounds until completion.
   - Executed `TestFinalReviewRealRawBounds`: an ordinary response consumes **33,825,217 raw bytes and returns 200 completed**; a 9 MiB unterminated streamed frame consumes **9,437,190 bytes** before producing 502. The latter error is too late to enforce a frame allocation bound.
   - Bound the adapter-specific raw reader/parser before buffering, enforcing cumulative raw SSE 32 MiB and frame 8 MiB in both JSON and streaming modes, and immediately cancel/close on failure. Keep existing output bounds as an additional boundary.

3. **[P2] Ordinary responses lose previously observed cumulative usage fields.**
   - References: `extensions/internal/protocol/adapter.go:106`; underlying `upstream/internal/upstream/sse.go:59`.
   - `Aggregate` replaces the entire usage map with each later map. The host's correct presence merger receives only the last map, so it cannot retain earlier known input/total counts.
   - Executed `TestFinalReviewRealPartialUsage`: `{prompt_tokens:5, completion_tokens:7, total_tokens:12}` followed by `{completion_tokens:0}` becomes `{input_tokens:null, output_tokens:0, total_tokens:null}`. Expected `5, 0, 12`. Ordinary and streamed results disagree for the same upstream events.
   - Preserve presence per field across the raw upstream stream in the strict adapted path, with zero replacing an earlier value and omitted fields retaining prior values. Add both-format real Handler ordinary/stream parity coverage.

4. **[P2] Claude streams complete with invalid tool input, including fabricated empty objects.**
   - References: `extensions/internal/protocol/stream.go:79`, `:343`; contrast the ordinary object check at `extensions/internal/protocol/adapter.go:252`.
   - Streaming validation records only whether an `arguments` field appeared. It never checks the final concatenated Claude input. RelayKit starts tool_use with `{}`, so an empty argument string becomes a completed empty object; partial/nonobject JSON also receives message_stop.
   - Executed `TestFinalReviewClaudeStreamArguments`: `""`, `{"q":`, and `[]` all reach `message_stop` with `stop_reason:tool_use`. The first produces no input_json_delta at all, leaving the manufactured `{}`. This contradicts the ordinary-path protection and generated-history object contract.
   - Accumulate bounded arguments per tool and require a complete non-null object for Claude before success; retain Responses raw-string semantics. Test fragmented valid objects, empty/truncated/nonobject inputs and overflow. Do not repair generated JSON.

5. **[P2] Text interleaved with a tool stream discards later argument fragments.**
   - References: `extensions/internal/protocol/stream.go:225`; pinned `extensions/third_party/relaykit/relayconvert/internal/oai_chat/to_claude_messages_resp.go:147`, `:148`, `:253`.
   - Splitting mixed chunks avoids one converter branch swallowing another, but the Claude converter clears its tool index/ID maps when switching from tools to text/thinking. A later continuation with only index/arguments becomes an unnamed pending tool and is omitted at finalization. The host's independent presence map still considers the original tool complete.
   - Executed `TestFinalReviewClaudeInterleavedTools`: call_a/check with fragment `{"q":`, then text `hello`, then the same index's `"hi"}` ends with message_stop while the last fragment is absent from the entire public stream. The same logical ordinary response retains full arguments.
   - Preserve live tool identity/argument association across mixed components, with a minimal RelayKit overlay or host sequencing that maintains a valid Claude block lifecycle. Test multi-chunk reasoning/text/tool interleaving and parallel tools through SDK aggregation/history. A silent successful drop is not an acceptable fallback.

6. **[P2] Advertised Responses inline files never reach the Handler.**
   - References: `extensions/internal/protocol/responses_validation.go:259`, `extensions/internal/protocol/adapter.go:41`; pinned `extensions/third_party/relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go:524` and `relayconvert/internal/media/media.go:31`; advertised at `README.md:128`.
   - Every input_file with data/URL calls the media resolver, but no production code configures it. This includes self-contained plaintext/PDF data URLs requiring no Files service or remote fetch.
   - Executed `TestFinalReviewInlineResponsesFile`: a valid `data:text/plain;base64,aGk=` inline note returns 400, “media resolver is not configured.” The planned library-convertible inline content capability is absent, not merely model-dependent.
   - Supply bounded in-memory resolution for the approved inline forms and test actual outgoing text/PDF payloads. Remote URL fetching/Files storage need not be added; unsupported external forms can remain explicit rejections. Do not mask this by claiming all file inputs are opaque references.

7. **[P2] Responses image detail is silently discarded.**
   - References: `extensions/internal/protocol/responses_validation.go:254`; pinned `extensions/third_party/relaykit/relayconvert/internal/oai_responses/to_oai_chat_req.go:485`.
   - The validator accepts `input_image.image_url` and does not guard `detail`; RelayKit immediately returns the URL string, skipping the sibling detail field without a diagnostic. The existing Chat image shape can represent `{url, detail}`.
   - Executed `TestFinalReviewResponsesImageDetail`: `detail:"high"` receives 200, but the actual outbound Handler payload contains only `image_url:"https://fixture.invalid/image.png"`. Caller-selected image processing intent has been changed silently.
   - Preserve and validate the representable detail value via a small library overlay; if a value cannot be expressed, reject it with its input path before contacting upstream. Test low/high/auto and malformed values at the outgoing boundary.

### Minor (Nice to Have)

- **Carry a permanent full-Handler history assertion into the SDK/mock suite.** `scripts/check_responses_sdk.py:63`, `scripts/check_anthropic_sdk.py:143`: these probes only establish acceptance. I independently executed `TestFinalReviewPairedHistorySurvivesFullHandler` for both formats and observed assistant plaintext reasoning/text, call ID/name/arguments, and paired tool result ID/content preserved in the actual outbound payload after PrepareBody. Thus the previously deferred item remains a coverage improvement, **not a demonstrated valid-history defect or an additional integration blocker**. Incorporate the observation assertion into the existing mock facility while adding the required regressions above.

## Focused reproduction and verification

With explicit controller permission, wrote only review artifacts under this directory. No tracked production files, index, branch, runtime credentials or services were changed. The overlay adds a test file to the already materialized core; before execution, all its protocol Go files and upstream SSE file were byte-compared (normalizing line endings) to the reviewed sources. Local toolchain: `go1.27.2 windows/amd64`; this focused reproduction does not replace the saved production Go 1.26.9 evidence.

```powershell
& .build/dev-tools/go/bin/go.exe -C .build/protocol-check-5-sdk-green test -mod=readonly -overlay D:/Code/workbuddy2api-ui/docs/superpowers/implementation/protocol-conversion/final-review-overlay.json ./internal/server -run '^TestFinalReview' -count=1 -v
```

Exact source: [final-review-repro_test.go](final-review-repro_test.go). Overlay: [final-review-overlay.json](final-review-overlay.json). Output: [final-review-repro.log](final-review-repro.log). Exit: [final-review-repro-exit.log](final-review-repro-exit.log), **1**. Seven targeted test groups fail on the seven findings; the two-format paired-history group passes. Upstream transport is an in-memory RoundTripper with synthetic credentials; no connection to fixture.invalid or any actual upstream is made. Existing broad suites and containers were not rerun.

## Recommendations

Address these as one complete correction wave, centered on the raw transport boundary and Claude stream state. Use the existing pool/Handler and overlay mechanism. Preserve legacy Chat semantics with a trusted internal opt-in for strict adapted requests rather than globally changing the frozen Handler's established behavior. Promote the reproductions to maintained regressions, then rerun affected test/vet/race, pinned SDK aggregation, and actual isolated gateway acceptance with fresh source/image identities.

## Declined to judge

Every behavior considered and set aside as outside this review's implementation scope:

- Real WorkBuddy/CodeBuddy model obedience, tool selection, media comprehension, reasoning availability and JSON Schema adherence: explicit upstream/model responsibilities; no real account calls authorized.
- Response persistence/retrieval/deletion, previous_response_id, Files storage, hosted/background tool execution, WebSockets and generated JSON repair: explicitly excluded; existing rejection boundaries remain appropriate.
- Exact thinking token-budget equivalence, signed/redacted/encrypted reasoning, Claude forced tool_choice/cache/citations/server-tool extensions: documented, controller-approved unsupported or approximate semantics; no expansion required.
- A remote media fetcher: not required to fix self-contained inline files; unsupported URLs can continue to fail explicitly without introducing network resolution.
- Global repair of the original OpenAI Handler's permissive normalization, orphan-tool cleanup, or provider-specific request rewriting: original OpenAI/no-bridge behavior must stay unchanged. Findings 1–3 concern the new adapted contract, not a request to redesign legacy Chat behavior. Valid paired history was independently checked.
- Unadvertised provider-specific Responses extensions (sampling/cache/moderation/vendor knobs beyond the approved conversion capabilities): no platform-completeness claim; they were not used to create speculative blockers. This does not excuse loss of the supported image detail in finding 7.
- Native installer builds/signing, macOS/Windows runtime process-tree acceptance and Windows-working-checkout Docker packaging: outside this feature's completed Linux delivery gate; existing reports correctly mark these unverified.
- Image publication, Git push, deployment/restart and general legal interpretation of combined licensing: not authorized here. This review verifies provenance, declaration retention and attribution implementation, not a legal opinion.

## Assessment

**Ready to merge? With fixes.** The architecture, packaging and happy-path SDK integration are sound, but the real Handler boundary currently defeats promised completion, usage and resource-limit semantics, and several tool/media conversions are observably incorrect. Passing prior unit/task gates does not supersede these real-boundary reproductions.

No broad suites, containers, native installers or real upstream calls were rerun; no release/deployment was performed. Unrelated `.gitignore`, deferred/probe evidence and `upstream-convert/` were preserved. The current risk is successful-looking responses with lost or incomplete content, plus unbounded upstream buffering before enforcement.
