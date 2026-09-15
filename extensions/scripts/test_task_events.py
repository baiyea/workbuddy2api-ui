import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import task_events
import task_common as tc
import task_runner as cat
import school_open_day_2026 as school


class TaskEventsTest(unittest.TestCase):
    def setUp(self):
        self.auth = {"uid": "same1234-full-uid", "token": "private-token", "nick": "private-nick"}
        self.opts = SimpleNamespace(yes=True, gap=1.5, only_claim=False, only_codes=["black_cat"])
        self.stats = {k: 0 for k in ("accounts", "total", "ok", "already", "skip", "pending", "fail", "credit", "energy")}
        self.out = io.StringIO()
        self.capture = contextlib.redirect_stdout(self.out)
        self.capture.__enter__()
        self.addCleanup(self.capture.__exit__, None, None, None)

    def events(self):
        return [json.loads(line.removeprefix("WB2A_TASK_EVENT ")) for line in self.out.getvalue().splitlines() if line.startswith("WB2A_TASK_EVENT ")]

    def test_emit_rejects_untrusted_fields_and_preserves_null(self):
        task_events.emit(self.auth["uid"], "unknown", "result_unconfirmed")
        self.assertEqual(self.events(), [{"uid": "same1234-full-uid", "status": "unknown", "detail": "result_unconfirmed", "reward": None}])
        for args in [("running", "query_failed", None), ("failed", "token=private-token", None), ("success", "reward_claimed", True), ("success", "reward_claimed", -1)]:
            with self.assertRaises(ValueError):
                task_events.emit(self.auth["uid"], *args)

    def test_cat_window_and_claimed_remain_skips(self):
        with patch.object(cat, "within_night_window", return_value=False), patch.object(tc, "do_post", side_effect=AssertionError("unexpected write")):
            cat.process_task(self.auth, "black_cat", {"accept_status": "accepted", "progress": {"current": 0, "target": 3}}, self.opts, self.stats)
            cat.process_task(self.auth, "black_cat", {"accept_status": "claimed", "progress": {"current": 3, "target": 3}}, self.opts, self.stats)
        self.assertEqual([(e["status"], e["detail"]) for e in self.events()], [("skipped", "outside_window"), ("skipped", "already_claimed")])

    def test_cat_claim_explicit_credit_not_energy_or_default_zero(self):
        with patch.object(cat.time, "sleep"):
            cat._claim_parse(self.auth, "black_cat", "same1234", self.stats, 1, 200, {"code": 0, "data": {"credit": 7, "energy": 99}})
            cat._claim_parse(self.auth, "black_cat", "same1234", self.stats, 1, 200, {"code": 0, "data": {"energy": 88}})
            cat._claim_parse(self.auth, "black_cat", "same1234", self.stats, 1, 200, {"code": 0, "data": {"credit": 7, "already_claimed": True}})
        self.assertEqual([e["reward"] for e in self.events()], [7, None, None])
        self.assertEqual(self.events()[-1]["status"], "skipped")

    def test_school_activity_closed_never_writes(self):
        def request(token, method, url, *args, **kwargs):
            self.assertEqual((method, url), ("GET", school.SCHOOL + "/tasks"))
            return 200, {"code": 0, "data": {"tasks": [], "in_period": False}}
        with patch.object(school, "_request", side_effect=request):
            school.run_account(self.auth, self.opts, self.stats)
        self.assertEqual([(e["status"], e["detail"]) for e in self.events()], [("skipped", "activity_closed")])

    def test_school_failure_then_lottery_reward_keeps_both_events(self):
        def request(token, method, url, *args, **kwargs):
            if url.endswith("/tasks"):
                return 200, {"code": 0, "data": {"in_period": True, "tasks": [{"task_code": "share_invite", "status": "pending", "progress": 0, "target_count": 1}]}}
            if url.endswith("/viewed"):
                return 403, {"code": 403, "message": "private-body"}
            if url.endswith("/config"):
                return 200, {"code": 0, "data": {"in_period": True, "chance": {"balance": 1}}}
            if url.endswith("/wheel/draw"):
                return 200, {"code": 0, "data": {"prize_code": "school_credit_6", "credit_amount": 6, "chance_balance": 0}}
            self.fail("unexpected upstream request")
        with patch.object(school, "_request", side_effect=request), patch.object(school.time, "sleep"):
            school.run_account(self.auth, self.opts, self.stats)
        self.assertEqual([(e["status"], e["reward"]) for e in self.events()], [("failed", None), ("success", 6)])
        self.assertNotIn("private", json.dumps(self.events()))

    def test_school_claimed_and_empty_chances_do_not_reward(self):
        def request(token, method, url, *args, **kwargs):
            self.assertEqual(method, "GET")
            if url.endswith("/tasks"):
                return 200, {"code": 0, "data": {"in_period": True, "tasks": [{"task_code": "share_invite", "status": "claimed"}]}}
            return 200, {"code": 0, "data": {"in_period": True, "chance": {"balance": 0}}}
        with patch.object(school, "_request", side_effect=request):
            school.run_account(self.auth, self.opts, self.stats)
        self.assertEqual([e["detail"] for e in self.events()], ["already_claimed", "no_chances"])
        self.assertTrue(all(e["reward"] is None for e in self.events()))

    def test_all_preserves_full_uids_and_skips_global_without_network(self):
        with tempfile.TemporaryDirectory() as directory:
            for index, (uid, realm) in enumerate([("same1234-one", "cn"), ("same1234-two", "cn"), ("global-user", "global")]):
                Path(directory, f"workbuddy-{index:08x}.json").write_text(json.dumps({"auth": {"accessToken": "private-token", "realm": realm}, "account": {"uid": uid}}))
            def get(auth, base, path, **kwargs):
                self.assertNotEqual(auth["uid"], "global-user")
                return 200, {"code": 0, "data": {"tasks": [{"task_code": "black_cat", "accept_status": "claimed"}]}}
            with patch.object(tc, "AUTHS", directory), patch.object(tc, "do_get", side_effect=get), patch.object(tc, "do_post", side_effect=AssertionError("unexpected write")), patch("sys.argv", ["task_runner.py", "ALL", "--yes", "--only", "black_cat"]):
                cat.main()
            self.assertEqual({e["uid"] for e in self.events()}, {"same1234-one", "same1234-two", "global-user"})

    def test_load_auth_stays_read_only(self):
        # Reject future loader writeback at the actual file-open boundary.
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "workbuddy-00000000.json")
            raw = json.dumps({"auth": {"accessToken": "private-token"}, "account": {"uid": "full-uid"}})
            path.write_text(raw)
            real_open = open
            def readonly(name, mode="r", *args, **kwargs):
                self.assertNotIn(mode, ("w", "a", "r+", "w+", "a+", "wb", "ab"))
                return real_open(name, mode, *args, **kwargs)
            with patch.object(tc, "AUTHS", directory), patch("builtins.open", side_effect=readonly):
                result = tc.load_auth("00000000")
            self.assertEqual(result["uid"], "full-uid")
            self.assertEqual(path.read_text(), raw)
            self.assertEqual(list(Path(directory).iterdir()), [path])

    def test_legacy_school_token_without_uid_still_skips_closed_activity(self):
        with patch.object(school, "_request", return_value=(200, {"code": 0, "data": {"in_period": False}})), patch("sys.argv", ["school_open_day_2026.py", "--token", "private-token", "--run", "--yes"]):
            with self.assertRaises(SystemExit) as stopped:
                school.main()
        self.assertEqual(stopped.exception.code, 0)

    def test_cat_one_failed_account_does_not_stop_next(self):
        with tempfile.TemporaryDirectory() as directory:
            for index, uid in enumerate(["first", "second"]):
                Path(directory, f"workbuddy-{index:08x}.json").write_text(json.dumps({"auth": {"accessToken": "private-token"}, "account": {"uid": uid}}))
            def get(auth, base, path, **kwargs):
                if auth["uid"] == "first":
                    return 403, {"code": 403, "message": "private-body"}
                return 200, {"code": 0, "data": {"tasks": [{"task_code": "black_cat", "accept_status": "claimed"}]}}
            with patch.object(tc, "AUTHS", directory), patch.object(tc, "do_get", side_effect=get), patch("sys.argv", ["task_runner.py", "ALL", "--yes", "--only", "black_cat"]):
                cat.main()
        self.assertEqual([(e["uid"], e["status"]) for e in self.events()], [("first", "failed"), ("second", "skipped")])

    def test_school_no_accounts_exits_without_network(self):
        with tempfile.TemporaryDirectory() as directory, patch.object(tc, "AUTHS", directory), patch("sys.argv", ["school_open_day_2026.py", "ALL", "--run", "--yes"]), patch.object(school, "_request", side_effect=AssertionError("unexpected request")):
            with self.assertRaises(SystemExit) as stopped:
                school.main()
        self.assertEqual(stopped.exception.code, 1)

    def test_school_partial_progress_is_not_task_success(self):
        reads = 0
        def request(token, method, url, *args, **kwargs):
            nonlocal reads
            if url.endswith("/tasks"):
                reads += 1
                task = {"task_code": "share_invite", "status": "in_progress", "progress": 0 if reads == 1 else 1, "target_count": 3}
                return 200, {"code": 0, "data": {"in_period": True, "tasks": [task]}}
            if url.endswith("/config"):
                return 200, {"code": 0, "data": {"in_period": True, "chance": {"balance": 0}}}
            self.assertTrue(url.endswith("/share-complete"))
            return 200, {"code": 0, "data": {}}
        with patch.object(school, "_request", side_effect=request), patch.object(school.time, "sleep"):
            school.run_account(self.auth, self.opts, self.stats)
        self.assertEqual([(e["status"], e["detail"]) for e in self.events()], [("unknown", "task_incomplete"), ("skipped", "no_chances")])


if __name__ == "__main__":
    unittest.main()
