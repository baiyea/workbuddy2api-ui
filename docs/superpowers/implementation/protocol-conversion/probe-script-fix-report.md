# Probe script follow-up fix

Scope: `scripts/probe_model_capabilities.py`, its unittest module, and the probe section of README. No protocol, vendor, Compose, or credential changes.

- Rename prompt content parameter so the Responses `text.format` keyword does not collide.
- Classify parsed SSE `error` / `response.failed` events as `error`.
- Require a `.json` output path and protect both JSON and sibling Markdown unless `--overwrite` is explicit.
- Only continue a protocol after its text baseline is `pass`; complete-but-empty/wrong outputs now skip subsequent calls, with at most one Chat diagnostic per model.

Validation:

- RED: new regressions produced the expected Schema TypeError, SSE classification failures, report overwrite failures, and baseline quota-guard failures (10 tests; 7 failed subcases and 1 error).
- GREEN: `python -m unittest discover -s scripts -p test_probe_model_capabilities.py -v`: 10 tests passed. Mocked default CLI run completes all 15 feature requests, including both tool returns, then writes completion evidence without the fixture secret. Unknown reasoning/usage remains `not_observed` and unsupported Messages Schema stays skipped.
- Broader check: `python -m unittest discover -s scripts -p 'test_*.py' -v`: 54 tests ran, 8 failed subcases and 30 errors in existing overlay/release tests; all 10 probe tests passed. This Windows-native run encountered Bash Windows-path handling, missing symlink privilege, Unix permission/mode/directory-fsync assumptions, Git fixture tracking/cleanup, CRLF, and release mock/path/concurrency expectations. No unrelated changes made to conceal these results.
- `git diff --check` passed (only Git LF/CRLF warnings).

Broader suite failure/error names (as emitted, including subcases):

- `ERROR: test_build_proxy_args_are_empty_by_default_and_use_only_explicit_environment (test_overlay.AcceptanceScriptTests.test_build_proxy_args_are_empty_by_default_and_use_only_explicit_environment)`
- `ERROR: test_candidate_fetch_resolves_requested_ref_without_default_branch_fallback (test_overlay.ExportSnapshotTests.test_candidate_fetch_resolves_requested_ref_without_default_branch_fallback)`
- `ERROR: test_materialize_applies_extension_and_ordered_patch (test_overlay.MaterializeTests.test_materialize_applies_extension_and_ordered_patch)`
- `ERROR: test_materialize_applies_patch_only_inside_destination_nested_in_git_repo (test_overlay.MaterializeTests.test_materialize_applies_patch_only_inside_destination_nested_in_git_repo)`
- `ERROR: test_materialize_rejects_damaged_lock_before_creating_destination (test_overlay.MaterializeTests.test_materialize_rejects_damaged_lock_before_creating_destination)`
- `ERROR: test_materialize_rejects_extension_below_symlink_parent (test_overlay.MaterializeTests.test_materialize_rejects_extension_below_symlink_parent)`
- `ERROR: test_materialize_rejects_extension_collision_and_cleans_new_destination (test_overlay.MaterializeTests.test_materialize_rejects_extension_collision_and_cleans_new_destination)`
- `ERROR: test_materialize_rejects_output_inside_all_input_trees_before_mutation (test_overlay.MaterializeTests.test_materialize_rejects_output_inside_all_input_trees_before_mutation) (case='resolved-upstream')`
- `ERROR: test_materialize_rejects_patch_alias_duplicate_and_symlink_parent (test_overlay.MaterializeTests.test_materialize_rejects_patch_alias_duplicate_and_symlink_parent) (series='change.patch\n./change.patch\n')`
- `ERROR: test_materialize_rejects_patch_alias_duplicate_and_symlink_parent (test_overlay.MaterializeTests.test_materialize_rejects_patch_alias_duplicate_and_symlink_parent) (series='linked/change.patch\n')`
- `ERROR: test_materialize_rejects_series_traversal_and_duplicates (test_overlay.MaterializeTests.test_materialize_rejects_series_traversal_and_duplicates) (series='../outside.patch\n')`
- `ERROR: test_materialize_rejects_series_traversal_and_duplicates (test_overlay.MaterializeTests.test_materialize_rejects_series_traversal_and_duplicates) (series='same.patch\nsame.patch\n')`
- `ERROR: test_materialize_without_overlay_is_reproducible_and_keeps_upstream_pristine (test_overlay.MaterializeTests.test_materialize_without_overlay_is_reproducible_and_keeps_upstream_pristine)`
- `ERROR: test_digest_hashes_symlink_text_without_following_it (test_overlay.SourceDigestTests.test_digest_hashes_symlink_text_without_following_it)`
- `ERROR: test_digest_rejects_escaping_symlink_nested_git_and_device (test_overlay.SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device) (build=<function SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device.<locals>.<lambda> at 0x000002011B633D80>)`
- `ERROR: test_digest_rejects_escaping_symlink_nested_git_and_device (test_overlay.SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device) (build=<function SourceDigestTests.test_digest_rejects_escaping_symlink_nested_git_and_device.<locals>.<lambda> at 0x000002011B6336A0>)`
- `ERROR: test_candidate_changed_by_validation_is_rejected_before_install (test_overlay.UpdateTests.test_candidate_changed_by_validation_is_rejected_before_install)`
- `ERROR: test_candidate_validation_failure_preserves_old_combination_and_runs_no_deploy_or_git_publish (test_overlay.UpdateTests.test_candidate_validation_failure_preserves_old_combination_and_runs_no_deploy_or_git_publish)`
- `ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:manifest')`
- `ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:duplicate-patch')`
- `ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:patch-conflict')`
- `ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:extension-collision')`
- `ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='check.sh:go-test')`
- `ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='acceptance.sh:image-build')`
- `ERROR: test_each_candidate_failure_keeps_snapshot_and_lock (test_overlay.UpdateTests.test_each_candidate_failure_keeps_snapshot_and_lock) (failure='acceptance.sh:runtime')`
- `ERROR: test_edit_after_candidate_staging_is_restored_instead_of_overwritten (test_overlay.UpdateTests.test_edit_after_candidate_staging_is_restored_instead_of_overwritten)`
- `ERROR: test_publish_failure_after_prior_backup_restores_current_pair (test_overlay.UpdateTests.test_publish_failure_after_prior_backup_restores_current_pair)`
- `ERROR: test_success_changes_only_snapshot_and_lock_and_uses_candidate_entrypoints (test_overlay.UpdateTests.test_success_changes_only_snapshot_and_lock_and_uses_candidate_entrypoints)`
- `ERROR: test_two_successful_updates_rotate_backup_to_the_previous_pair (test_overlay.UpdateTests.test_two_successful_updates_rotate_backup_to_the_previous_pair)`
- `ERROR: test_timestamp_pair_is_accepted_then_pushed_and_pulled (test_release.ReleaseTests.test_timestamp_pair_is_accepted_then_pushed_and_pulled)`
- `FAIL: test_export_uses_committed_blobs_and_preserves_license_and_modes (test_overlay.ExportSnapshotTests.test_export_uses_committed_blobs_and_preserves_license_and_modes)`
- `FAIL: test_windows_checkout_preserves_snapshot_digest_and_rejects_content_changes (test_overlay.MaterializeTests.test_windows_checkout_preserves_snapshot_digest_and_rejects_content_changes)`
- `FAIL: test_digest_tracks_content_and_executable_mode (test_overlay.SourceDigestTests.test_digest_tracks_content_and_executable_mode)`
- `FAIL: test_journal_unlink_failure_keeps_new_pair_and_complete_old_backup (test_overlay.UpdateTests.test_journal_unlink_failure_keeps_new_pair_and_complete_old_backup)`
- `FAIL: test_bash_entry_works_outside_repository (test_release.ReleaseTests.test_bash_entry_works_outside_repository)`
- `FAIL: test_checks_acceptance_push_or_pull_failure_preserves_compose (test_release.ReleaseTests.test_checks_acceptance_push_or_pull_failure_preserves_compose) (failure='scripts/check.sh')`
- `FAIL: test_checks_acceptance_push_or_pull_failure_preserves_compose (test_release.ReleaseTests.test_checks_acceptance_push_or_pull_failure_preserves_compose) (failure='scripts/acceptance.sh')`
- `FAIL: test_existing_tag_or_registry_failure_never_publishes (test_release.ReleaseTests.test_existing_tag_or_registry_failure_never_publishes) (options={'concurrent': True})`

No real requests, real credentials, builds, service restarts, image publication, deployment, or Git push were performed by this fix task. Existing unrelated workspace changes were preserved. Offline regression success does not establish real model capabilities; the controller owns real-call verification and independent rereview.
