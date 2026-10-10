# Shared transport integration notes

These are code-reading findings, not proof of completed implementation. The approved spec/brief controls scope.

- Use public relayconvert registry APIs, preserving the same per-request convmeta.Values for request/response/stream. Custom/namespace tools store request state there.
- NewResponseStreamState accepts ResponseStreamOptions{Model: model, EmitSequenceNumber: true}. ConvertStreamResponseChunk already increments response count; do not increment twice.
- Stream Value is relayconvert.ChatToResponsesStreamEvent (Type/Payload) for Responses and *dto.ClaudeResponse for Claude. Inspect actual definitions before using them.
- Library usage uses integer fields and MergeUsageNonZero, losing missing/zero semantics. Preserve raw JSON presence separately; replace serialized public usage only, do not invent counts.
- Feed content chunks without Usage or FinishReason. Consume usage-only chunks locally. Once Handler returns with validated normal finish, feed a synthetic empty delta containing finish_reason, then Finalize. This prevents Claude early completion and lets late usage override previous values.
- Claude converter handles reasoning/text/tools with branching, so split a mixed incoming delta into ordered components before passing it to the library.
- Library Responses state accumulates text/tool arguments for final output; enforce total stream input bound as well as per-frame bound.
- ConvertRequest safe loss policy does not cover all media diagnostics added later. Explicitly apply types.RejectConversionLoss to aggregate result.Diagnostics.
- Typed top-level DTO validation does not validate RawMessage/any content. Reject null/invalid shapes and unsupported state before next. Existing Anthropic boundary tests show required current security/limit behavior; enhanced validation belongs Task 4.
- Existing extensions/internal/anthropic/adapter.go and stream.go provide bounded synchronous ResponseWriter, cancellation and private context patterns; reuse relevant transport ideas, do not rewrite protocol mappings.
- Vendor snapshot remains byte-for-byte source. History mapping fixes are separate later patches, not changes to this transport task.

Environment: portable Go at .build/dev-tools/go/bin/go.exe (1.27.2); gcc available on PATH. Git Bash is C:/Program Files/Git/bin/bash.exe; Docker engine now available. Use fresh overlay directories for every source revision.
