# Task 1 report — RelayKit snapshot and build integration

Status: DONE_WITH_CONCERNS — Task1 implementation and primary native/Go1.26 checks passed. Broadened native scripts/deploy suites have recorded Windows limitations; complete image/gateway acceptance remains Task5.

## Scope and changed files

- `extensions/third_party/relaykit/`: unchanged canonical Git blobs for 159 independently tracked module files, plus original New API root `LICENSE`, `NOTICE`, `THIRD-PARTY-LICENSES.md`; `SOURCE.json` records provenance and SHA-256 for all 162 original files. No New API host application or credentials copied. Full original-file inventory is in SOURCE.json.
- `.gitattributes`: preserve vendor bytes with `-text`; exclude unified-patch context spaces from whitespace diagnostics. Ordinary source whitespace checks remain active.
- `patches/0008-relaykit-module.patch`, `patches/series`, `patches/README.md`: core Go 1.26, local require `github.com/QuantumNous/new-api/relaykit v0.0.0`, replace `./third_party/relaykit`, dependency checksums and maintenance instructions.
- `deploy/core.Dockerfile`: core build Go 1.26 series, nested-module test/vet, declarations at `/app/licenses/relaykit/`. Console toolchain and Compose unchanged.
- `scripts/check.sh`: nested-module test/vet before existing checks, with no references to future protocol packages.
- `desktop/scripts/prepare.py`, `desktop/scripts/test_prepare.py`: copy only LICENSE/NOTICE/dependency notices/SOURCE.json into `licenses/relaykit`; real copying regression checks exact bytes and absence of module source.
- This report. Original `.gitignore`, probe JSON, deferred draft and `upstream-convert/` preserved; root's `task-2-notes.md` excluded.

## Source audit

Actual commands: `git -C upstream-convert rev-parse HEAD`, `git -C upstream-convert status --short`, `git -C upstream-convert ls-files relaykit LICENSE NOTICE THIRD-PARTY-LICENSES.md`.

HEAD was `0f38a14a5e8e322ea741478b0e49878be3130e1d`, status empty. Export used `git show COMMIT:PATH` for each tracked file, checked working bytes modulo checkout CRLF against that committed blob, and wrote canonical blob bytes. No draft restoration and no source-directory edits. Original module stays `github.com/QuantumNous/new-api/relaykit`, `go 1.25.1`.

SOURCE.json SHA-256: `43383263acee42a6d6f71bba80b4c6f29a9519a6e09d6c0d9bd6ea6bf8af36e5`. Each original-file digest is independently listed in it.

A Windows index export with `git -c core.autocrlf=true checkout-index --prefix=ABS_NEW_DIRECTORY/ --stdin -z` matched all 162 SHA-256 values. A final second export after LF-normalizing our generated SOURCE.json matched all 162 original hashes and SOURCE.json bytes; exit 0. The unchanged vendor SOURCE.json is excluded from its own original-file inventory.

## TDD evidence

RED: `python -m unittest discover -s desktop/scripts -p test_prepare.py -v` exited 1: 10 tests, exactly one failure, `test_resources_include_relaykit_notices_and_source_provenance`. Expected assertion: `desktop resources must copy RelayKit notices and provenance`; the copier did not exist yet. Other 9 tests passed.

GREEN: same command exited 0: 10 tests passed after the seven-line copier and its prepare() call were added. Fixture includes CRLF LICENSE bytes, dependency notices, SOURCE.json, and unwanted go.mod/converter.go; the exact four packaged files and byte equality are asserted. No copy or filesystem mocks.

Broader desktop command `python -m unittest discover -s desktop/scripts -p 'test_*.py' -v`: exit 0, 11 tests.

## Actual integration checks

Native Go was `D:/Code/workbuddy2api-ui/.build/dev-tools/go/bin/go.exe`, version `go1.27.2 windows/amd64`.

1. `python scripts/overlay.py prepare --output .build/protocol-deps-1`: exit 0, previously nonexistent directory. Then quoted PowerShell arguments `go -C .build/protocol-deps-1 mod edit '-go=1.26' '-require=github.com/QuantumNous/new-api/relaykit@v0.0.0' '-replace=github.com/QuantumNous/new-api/relaykit=./third_party/relaykit'`.
2. `go -C .build/protocol-deps-1 mod download all`: official proxy attempt stalled and was stopped; command rerun with process-local `GOPROXY=https://goproxy.cn,direct`, exit 0. No persistent host configuration changed and no remote RelayKit version selected. Unified go.mod/go.sum patch generated from this directory against the frozen base.
3. `python scripts/overlay.py prepare --output .build/protocol-check-1`: exit 0. `go -C .build/protocol-check-1 list -m -json github.com/QuantumNous/new-api/relaykit`: exit 0, version v0.0.0, local directory `.build/protocol-check-1/third_party/relaykit`, GoVersion1.25.1.
4. `go -C .build/protocol-check-1/third_party/relaykit test ./...`: exit 0, all 9 test-bearing packages passed. Independent `vet ./...`: exit 0.
5. `go -C .build/protocol-check-1 test ./...`: exit 0, all 21 test-bearing core packages passed. `go -C .build/protocol-check-1 vet ./...`: exit 0.
6. `python scripts/overlay.py prepare --output .build/protocol-check-2`: exit 0 after generated SOURCE.json LF normalization; same module source and dependency patch.
7. `git diff --cached --check`: initially flagged our generated CRLF manifest and ordinary patch context whitespace. Manifest rewritten as LF and patch whitespace attributes added; recheck exit 0. No original vendor file was modified to silence diagnostics.
8. Native project command, with process-local PATH containing Git Bash and our Go binary, and process-local GOPROXY: `C:/Program Files/Git/bin/bash.exe scripts/check.sh`: exit 0. Nested RelayKit test/vet, core full test/vet, seven existing core race packages, console race, Node33/33, materialized task Python15/15 all passed.
9. `go -C .build/protocol-check-2 test -mod=readonly github.com/QuantumNous/new-api/relaykit/relayconvert`: exit 0. Core can consume the registry with a read-only module graph; no dependency edits required.
10. First disposable Go1.26 container preparation was aborted while `apk add python3 git` waited on direct outbound access; Go checks had not started. Only that task-owned container was stopped; no Go failure claimed. Second container uses Go1.26.9, read-only prepared-source and cached dependency mounts, GOTOOLCHAIN=local/GOPROXY=off, copies source to container scratch. Command `docker run --rm --mount type=bind,source=D:/Code/workbuddy2api-ui/.build/protocol-check-2,target=/input,readonly --mount type=bind,source=C:/Users/g_200/go/pkg/mod,target=/go/pkg/mod,readonly -e GOTOOLCHAIN=local -e GOPROXY=off golang:1.26-alpine sh -ec 'cp -a /input /work; cd /work; go version; go -C third_party/relaykit test ./...; go -C third_party/relaykit vet ./...; go test -mod=readonly github.com/QuantumNous/new-api/relaykit/relayconvert; go test ./...; go vet ./...'`: exit 0. Go1.26.9 Linux nested test/vet, read-only import and all21 core packages plus vet passed. No network downloads in this successful run.

## Broadened native Python limitations

`python -m unittest discover -s scripts -p 'test_*.py' -v`: exit 1, 44 tests, 8 failures/30 errors, both with initial PATH and rerun with Git Bash first. Existing tests require POSIX executable bits, symlink privileges, directory fsync, or POSIX shell/path matching. These were recorded rather than changed outside Task1. Local full output: `task-1-native-scripts.log` (ignored build evidence). Failures by name:

```
ERROR: test_build_proxy_args_are_empty_by_default_and_use_only_explicit_environment (test_overlay.AcceptanceScriptTests.test_build_proxy_args_are_empty_by_default_and_use_only_explicit_environment)
ERROR: test_candidate_fetch_resolves_requested_ref_without_default_branch_fallback (test_overlay.ExportSnapshotTests.test_candidate_fetch_resolves_requested_ref_without_default_branch_fallback)
ERROR: test_materialize_applies_extension_and_ordered_patch (test_overlay.MaterializeTests.test_materialize_applies_extension_and_ordered_patch)
ERROR: test_materialize_applies_patch_only_inside_destination_nested_in_git_repo (test_overlay.MaterializeTests.test_materialize_applies_patch_only_inside_destination_nested_in_git_repo)
ERROR: test_materialize_rejects_damaged_lock_before_creating_destination (test_overlay.MaterializeTests.test_materialize_rejects_damaged_lock_before_creating_destination)
ERROR: test_materialize_rejects_extension_below_symlink_parent (test_overlay.MaterializeTests.test_materialize_rejects_extension_below_symlink_parent)
ERROR: test_materialize_rejects_extension_collision_and_cleans_new_destination (test_overlay.MaterializeTests.test_materialize_rejects_extension_collision_and_cleans_new_destination)
ERROR: test_materialize_rejects_output_inside_all_input_trees_before_mutation (test_overlay.MaterializeTests.test_materialize_rejects_output_inside_all_input_trees_before_mutation) (case='resolved-upstream')
ERROR: test_materialize_rejects_patch_alias_duplicate_and_symlink_parent (test_overlay.MaterializeTests.test_materialize_rejects_patch_alias_duplicate_and_symlink_parent) (series='change.patch\n./change.patch\n')
ERROR: test_materialize_rejects_patch_alias_duplicate_and_symlink_parent (test_overlay.MaterializeTests.test_materialize_rejects_patch_alias_duplicate_and_symlink_parent) (series='linked/change.patch\n')
ERROR: test_materialize_rejects_series_traversal_and_duplicates (test_overlay.MaterializeTests.test_materialize_rejects_series_traversal_and_duplicates) (series='../outside.patch\n')
ERROR: test_materialize_rejects_series_traversal_and_duplicates (test_overlay.MaterializeTests.test_materialize_rejects_series_traversal_and_duplicates) (series='same.patch\nsame.patch\n')
ERROR: test_materialize_without_overlay_is_reproducible_and_keeps_upstream_pristine (test_overlay.MaterializeTests.test_materialize_without_overlay_is_reproducible_and_keeps_upstream_pristine)
ERROR: test_digest_hashes_symlink_text_without_following_it (test_overlay.SourceDigestTests.test_digest_hashes_symlink_text_without_following_it)
ERROR: test_digest_rejects_escaping_symlink_nested_git_and_device (test_overlay.SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device) (build=<function SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device.<locals>.<lambda>>)
ERROR: test_digest_rejects_escaping_symlink_nested_git_and_device (test_overlay.SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device) (build=<function SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device.<locals>.<lambda>>)
ERROR: test_candidate_changed_by_validation_is_rejected_before_install (test_overlay.UpdateTests.test_candidate_changed_by_validation_is_rejected_before_install)
ERROR: test_candidate_validation_failure_preserves_old_combination_and_runs_no_deploy_or_git_publish (test_overlay.UpdateTests.test_candidate_validation_failure_preserves_old_combination_and_runs_no_deploy_or_git_publish)
ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:manifest')
ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:duplicate-patch')
ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:patch-conflict')
ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:extension-collision')
ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:go-test')
ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='acceptance.sh:image-build')
ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='acceptance.sh:runtime')
ERROR: test_edit_after_candidate_staging_is_restored_instead_of_overwritten (test_overlay.UpdateTests.test_edit_after_candidate_staging_is_restored_instead_of_overwritten)
ERROR: test_publish_failure_after_prior_backup_restores_current_pair (test_overlay.UpdateTests.test_publish_failure_after_prior_backup_restores_current_pair)
ERROR: test_success_changes_only_snapshot_and_lock_and_uses_candidate_entrypoints (test_overlay.UpdateTests.test_success_changes_only_snapshot_and_lock_and_uses_candidate_entrypoints)
ERROR: test_two_successful_updates_rotate_backup_to_the_previous_pair (test_overlay.UpdateTests.test_two_successful_updates_rotate_backup_to_the_previous_pair)
ERROR: test_timestamp_pair_is_accepted_then_pushed_and_pulled (test_release.ReleaseTests.test_timestamp_pair_is_accepted_then_pushed_and_pulled)
FAIL: test_export_uses_committed_blobs_and_preserves_license_and_modes (test_overlay.ExportSnapshotTests.test_export_uses_committed_blobs_and_preserves_license_and_modes)
FAIL: test_windows_checkout_preserves_snapshot_digest_and_rejects_content_changes (test_overlay.MaterializeTests.test_windows_checkout_preserves_snapshot_digest_and_rejects_content_changes)
FAIL: test_digest_tracks_content_and_executable_mode (test_overlay.SourceDigestTests.test_digest_tracks_content_and_executable_mode)
FAIL: test_journal_unlink_failure_keeps_new_pair_and_complete_old_backup (test_overlay.UpdateTests.test_journal_unlink_failure_keeps_new_pair_and_complete_old_backup)
FAIL: test_bash_entry_works_outside_repository (test_release.ReleaseTests.test_bash_entry_works_outside_repository)
FAIL: test_checks_acceptance_push_or_pull_failure_preserves_compose (test_release.ReleaseTests.test_checks_acceptance_push_or_pull_failure_preserves_compose) (failure='scripts/check.sh')
FAIL: test_checks_acceptance_push_or_pull_failure_preserves_compose (test_release.ReleaseTests.test_checks_acceptance_push_or_pull_failure_preserves_compose) (failure='scripts/acceptance.sh')
FAIL: test_existing_tag_or_registry_failure_never_publishes (test_release.ReleaseTests.test_existing_tag_or_registry_failure_never_publishes) (options={'concurrent': True})
```

`python -m unittest discover -s deploy -p 'test_*.py' -v`: exit 1, 13 tests, exactly 2 failures: `test_runtime_needs_only_compose_and_never_builds_or_mounts_source` (Windows path separators), `test_backup_archives_confirmed_bind_mounts_with_hashes_and_restricted_modes` (POSIX chmod expectation on NTFS). Other 11 passed. Full Linux scripts/deploy validation belongs to final Task5 integration.

## Safety and remaining verification

`git diff -- upstream upstream.lock` and source status were empty after work; all original vendor manifest hashes matched. No actual accounts/upstream calls, service restart, push, publication or deployment occurred. Docker test uses only a read-only prepared core mount, copies it to container scratch and removes the container after completion. Full image packaging/isolated gateway acceptance and native desktop installer checks are deferred to final integration; this Task1 does not claim those passed.
