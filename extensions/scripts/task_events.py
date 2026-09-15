"""Fixed, credential-free observations; task decisions remain in upstream scripts."""
import json

DETAILS = {
    "reward_claimed", "already_claimed", "claim_failed", "outside_window",
    "activity_closed", "task_unavailable", "manual_required", "task_incomplete",
    "report_failed", "query_failed", "action_failed", "task_progress",
    "no_chances", "lottery_failed", "lottery_stalled", "global", "result_unconfirmed",
}


def emit(uid, status, detail, reward=None):
    if status not in {"success", "failed", "skipped", "unknown"}:
        raise ValueError("invalid task status")
    # Legacy --token permits no --uid. Such events cannot match a core snapshot UID.
    if not isinstance(uid, str) or len(uid) > 256 or detail not in DETAILS:
        raise ValueError("invalid task classification")
    if reward is not None and (type(reward) is not int or reward < 0 or reward > 2**63 - 1):
        raise ValueError("invalid task reward")
    print("WB2A_TASK_EVENT " + json.dumps({"uid": uid, "status": status,
          "detail": detail, "reward": reward}, ensure_ascii=False), flush=True)
