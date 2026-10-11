# Final correction wave — seven verified integration findings

Read `final-review.md` first for the complete findings and exact reproductions. Fix all seven Important findings together, plus promote the already-passing full-Handler paired-history observation into a maintained regression (the one Minor). The authoritative spec remains `docs/superpowers/specs/2026-10-10-protocol-conversion.md`; current AGENTS and this task's scope apply.

## Binding constraints

- Reuse RelayKit's public conversion semantics; never introduce a second converter, service, pool, HTTP loopback, scheduler, file store or generated-JSON repair.
- Do not modify `upstream/`, original `extensions/third_party/relaykit/`, or `upstream-convert/`. New core files go under extensions; edits to original core/library files use ordered overlay patches with regressions and maintenance/removal notes.
- Keep original OpenAI Chat/no-bridge behavior. A trusted in-process adapter opt-in may enable strict raw upstream handling for Responses/Messages before original normalization; public headers/body must not forge this mode or conversation/owner.
- Enforce raw SSE frame8MiB and cumulative32MiB before unbounded allocation, in both JSON and streaming adapted paths; keep existing output limits. Read errors/invalid termination must prevent success and cancel/close promptly.
- Preserve real usage presence/zero/last cumulative values through full Handler; do not estimate.
- Preserve Claude fragmented/parallel/interleaved tools through SDK aggregation and history. Final Claude tool arguments must be a complete non-null JSON object; Responses retains raw argument-string semantics. Never repair generated arguments.
- Support bounded self-contained inline text/PDF media using the public resolver contract. No remote fetcher or Files/storage service. Unsupported external media must fail explicitly before upstream.
- Preserve and validate representable Responses image detail low/high/auto; no silent discard.
- Preserve existing body/auth/owner/CSRF/cancellation/version/beta checks, Compose and actual services. No real upstream calls, push, publication, deployment or new branch/worktree.
- Preserve user root.gitignore/untracked upstream-convert/probe/deferred files and controller docs. Stage only your exact paths. All intermediate reports/logs under docs/superpowers.

## Execution / evidence

- No subagents. Follow Superpowers TDD/systematic debugging and implementer report contract. This is the single final-review correction wave; self-review all findings and affected interactions before returning.
- Repro source/log/Go overlay are in this directory (`final-review-repro_test.go`, `final-review-overlay.json`, `final-review-repro.log`). Promote focused real-Handler cases to maintained tests, first reproduce RED, then implement minimal changes and capture GREEN. Keep synthetic in-memory RoundTripper/mock credentials only.
- Retain existing SDK assertions and add full-Handler/mock observations for the corrected boundaries, including parallel/interleaved tool aggregation and paired history. Do not make rejected valid input the substitute for required supported semantics.
- Run fresh materializations. Relevant library/core tests, vet and race must pass. Because raw transport and converter behavior changes, run final full `scripts/check.sh`, required Python scripts/deploy/desktop, Compose config, and actual isolated acceptance with both pinned SDKs enabled against newly built images.
- Read `task-5-environment-notes.md`, `task-5-report.md` verification/environment section and ignored `linux-runner-context.json` before container work. Existing SDK image/volume/toolchains are prepared. Use LF committed source archive, identical daemon mountpoint/TMPDIR, hostnetwork/socket, bash-c. Go production baseline1.26; SDK Python3.12/anthropic0.67.0/openai2.6.1/httpx0.28.1. Per-build explicit proxy permitted, never global.
- Root owns subsequent scoped re-review, final completion audit and cleanup. Do not claim those gates done. Keep task-owned verification volume for root; normal acceptance cleanup remains required.
- Report to `final-fix-report.md`: each finding, precise changes, RED/GREEN commands/results, final source/image identities, full integration evidence, callers checked, remaining limits. Commit own changes and report. Return concise status/commits/tests/concerns/path, with milestone updates during work.
