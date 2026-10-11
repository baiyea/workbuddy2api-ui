import argparse
import contextlib
import io
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch

from probe_model_capabilities import parse_models, parse_sse, summarize, check_schema, run, main


class ProbeTests(unittest.TestCase):
    def test_default_matrix_finishes_every_request_branch_without_network(self):
        marker, reply = "probe_1234567890", "result_1234567890"
        results = []
        for protocol in ("responses", "anthropic"):
            tool = {"id": "call_fixture", "name": "probe_echo", "arguments": {"value": marker}}
            block = ({"type": "function_call", "call_id": tool["id"], "name": tool["name"],
                      "arguments": json.dumps(tool["arguments"])} if protocol == "responses" else
                     {"type": "tool_use", "id": tool["id"], "name": tool["name"], "input": tool["arguments"]})
            texts = [marker, "437", marker, None, reply]
            if protocol == "responses":
                texts.append('{"marker":"schema_only_47","count":7}')
            texts.extend(["red,blue", marker])
            for text in texts:
                results.append({"http_status": 200, "elapsed_ms": 1, "summary": {
                    "complete": True, "text": text or "", "reasoning_chars": 0, "usage": None,
                    "tools": [tool] if text is None else [], "output": [block] if text is None else [],
                    "errors": []}})
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "results.json"
            argv = ["probe", "--base-url", "http://localhost:1/v1", "--model", "Fixture=exact:model-id",
                    "--output", str(output)]
            with patch("sys.argv", argv), patch.dict("os.environ", {"WB2A_API_KEY": "fixture-secret"}), \
                    patch("probe_model_capabilities.uuid.uuid4", return_value=SimpleNamespace(hex="1234567890")), \
                    patch("probe_model_capabilities.call", side_effect=results), \
                    contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(main(), 0)
            raw = output.read_text(encoding="utf-8")
            report = json.loads(raw)
            self.assertIn("completed_at", report)
            self.assertNotIn("fixture-secret", raw)
            self.assertTrue(output.with_suffix(".md").exists())
            self.assertEqual(report["request_retries"], 0)
            requests = {(r["protocol"], r["check"]): r for r in report["records"] if "request" in r}
            self.assertEqual(list(requests), [
                ("responses", check) for check in ("text", "stream", "history", "tool_call", "tool_result",
                                                  "strict_schema_conflict", "image", "inline_text_file")
            ] + [("anthropic", check) for check in ("text", "stream", "history", "tool_call", "tool_result",
                                                    "image", "inline_text_file")])
            for record in requests.values():
                self.assertEqual(record["status"], "pass")
                self.assertEqual(record["model_name"], "Fixture")
                self.assertEqual(record["model_id"], "exact:model-id")
                self.assertEqual(record["request"]["model"], "exact:model-id")
            schema = requests["responses", "strict_schema_conflict"]["request"]
            self.assertIsInstance(schema["input"], str)
            self.assertTrue(schema["text"]["format"]["strict"])
            responses_history = requests["responses", "tool_result"]["request"]["input"]
            self.assertEqual(responses_history[1]["call_id"], "call_fixture")
            self.assertEqual(responses_history[2], {"type": "function_call_output", "call_id": "call_fixture", "output": reply})
            messages_history = requests["anthropic", "tool_result"]["request"]["messages"]
            self.assertEqual(messages_history[1]["content"][0]["id"], "call_fixture")
            self.assertEqual(messages_history[2]["content"][0], {"type": "tool_result", "tool_use_id": "call_fixture", "content": reply})
            observations = [r for r in report["records"] if r["check"] in ("reasoning_observed", "usage_observed")]
            self.assertEqual([r["status"] for r in observations], ["not_observed"] * 4)
            self.assertEqual([r["status"] for r in report["records"] if r["protocol"] == "anthropic" and r["check"] == "strict_schema_conflict"], ["skipped"])

    def test_sse_errors_are_reported_as_errors(self):
        for kind in ("error", "response.failed"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / "results.json"
                args = argparse.Namespace(model=["Fixture=fixture-model"], key_file=None,
                                          base_url="http://localhost:1", output=output, overwrite=False,
                                          checks=["stream"], max_output_tokens=64, timeout=1)
                def fake_call(base, key, protocol, body, timeout):
                    event = {"type": kind if protocol == "responses" else "error", "error": {"message": "fixture failure"}}
                    return {"http_status": 200, "elapsed_ms": 1,
                            "summary": summarize(protocol, [{"event": event["type"], "data": event}], True)}
                with patch.dict("os.environ", {"WB2A_API_KEY": "fixture-secret"}), \
                        patch("probe_model_capabilities.call", side_effect=fake_call), contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(run(args), 1)
                records = json.loads(output.read_text(encoding="utf-8"))["records"]
                self.assertEqual([r["status"] for r in records if r["check"] == "stream"], ["error", "error"])

    def test_completed_but_wrong_baseline_skips_expensive_requests(self):
        for text in ("", "wrong marker"):
            with self.subTest(text=text), tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / "results.json"
                args = argparse.Namespace(model=["Fixture=fixture-model"], key_file=None,
                                          base_url="http://localhost:1", output=output, overwrite=False,
                                          checks=["text", "stream", "history"], max_output_tokens=64, timeout=1)
                result = {"http_status": 200, "elapsed_ms": 1,
                          "summary": {"complete": True, "text": text, "reasoning_chars": 0, "usage": None, "errors": []}}
                with patch.dict("os.environ", {"WB2A_API_KEY": "fixture-secret"}), \
                        patch("probe_model_capabilities.call", return_value=result), contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(run(args), 1)
                records = json.loads(output.read_text(encoding="utf-8"))["records"]
                self.assertEqual([(r["protocol"], r["check"], r["status"]) for r in records], [
                    ("responses", "text", "fail"), ("chat", "diagnostic_baseline", "fail"),
                    ("responses", "remaining", "skipped"), ("anthropic", "text", "fail"),
                    ("anthropic", "remaining", "skipped")])

    def test_output_requires_json_and_protects_both_report_files(self):
        for filename, existing, overwrite in (("results.md", None, False), ("results.md", None, True),
                                               ("results.json", "results.json", False),
                                               ("results.json", "results.md", False)):
            with self.subTest(filename=filename, existing=existing, overwrite=overwrite), tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / filename
                if existing:
                    (Path(directory) / existing).write_text("keep this evidence", encoding="utf-8")
                args = argparse.Namespace(model=["Fixture=fixture-model"], key_file=None,
                                          base_url="http://localhost:1", output=output, overwrite=overwrite,
                                          checks=[], max_output_tokens=64, timeout=1)
                with patch.dict("os.environ", {"WB2A_API_KEY": "fixture-secret"}):
                    with self.assertRaises(ValueError):
                        run(args)
                if existing:
                    self.assertEqual((Path(directory) / existing).read_text(encoding="utf-8"), "keep this evidence")
                    args.overwrite = True
                    with patch.dict("os.environ", {"WB2A_API_KEY": "fixture-secret"}):
                        self.assertEqual(run(args), 0)
                    self.assertIn("completed_at", json.loads(output.read_text(encoding="utf-8")))
                    self.assertNotIn("keep this evidence", output.with_suffix(".md").read_text(encoding="utf-8"))
                else:
                    self.assertFalse(output.exists())

    def test_model_labels_and_ids(self):
        self.assertEqual(parse_models(["DeepSeek=cn:deepseek-v4.1-flash"]),
                         [{"name": "DeepSeek", "id": "cn:deepseek-v4.1-flash"}])
        for bad in (["=cn:x"], ["label="], ["a=cn:x", "b=cn:x"]):
            with self.assertRaises(ValueError):
                parse_models(bad)

    def test_sse_multiline_and_error_not_success(self):
        events = parse_sse(': heartbeat\r\nevent: response.completed\r\ndata: {"type":\r\ndata: "response.completed", "response":{"status":"completed","output":[]}}\r\n\r\n')
        self.assertEqual(events[0]["data"]["type"], "response.completed")
        events.append({"event": "error", "data": {"type": "error", "error": {"message": "failed"}}})
        self.assertFalse(summarize("responses", events, True)["complete"])
        with self.assertRaises(ValueError):
            parse_sse('data: {"type":"response.completed"}')

    def test_anthropic_fragmented_tool_and_late_zero(self):
        data = [
            {"type": "message_start", "message": {"content": [], "usage": {"input_tokens": 5, "output_tokens": 2}}},
            {"type": "content_block_start", "index": 0, "content_block": {"type": "tool_use", "id": "call_a", "name": "probe_echo", "input": {}}},
            {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": '{"value":'}},
            {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": '"hello"}'}},
            {"type": "content_block_stop", "index": 0},
            {"type": "message_delta", "delta": {"stop_reason": "tool_use"}, "usage": {"output_tokens": 0}},
            {"type": "message_stop"},
        ]
        result = summarize("anthropic", [{"event": x["type"], "data": x} for x in data], True)
        self.assertTrue(result["complete"])
        self.assertEqual(result["tools"][0]["arguments"], {"value": "hello"})
        self.assertEqual(result["usage"], {"input_tokens": 5, "output_tokens": 0})
        self.assertFalse(summarize("anthropic", [{"event": x["type"], "data": x} for x in data[:-1]], True)["complete"])

    def test_schema_checks_constraints_not_just_valid_json(self):
        self.assertTrue(check_schema('{"marker":"schema_only_47","count":7}'))
        for invalid in ('{}', '{"marker":"schema_only_47","count":true}',
                        '{"marker":"schema_only_47","count":7,"extra":1}', 'plain text'):
            self.assertFalse(check_schema(invalid))

    def test_chat_baseline_requires_finish_and_actual_text(self):
        reply = {"choices": [{"finish_reason": "stop", "message": {"content": "probe_ok"}}], "usage": {"total_tokens": 4}}
        self.assertTrue(summarize("chat", reply, False)["complete"])
        reply["choices"][0]["finish_reason"] = None
        self.assertFalse(summarize("chat", reply, False)["complete"])

    def test_failed_baseline_skips_expensive_cases_and_reports_failure(self):
        calls = []

        def fake_call(base, key, protocol, body, timeout):
            calls.append(protocol)
            return {"http_status": 502, "elapsed_ms": 1, "error": "fixture transport failure"}

        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "results.json"
            key = Path(directory) / "key.json"
            key.write_text('{"api_key":"fixture-secret"}')
            args = argparse.Namespace(model=["Fixture=fixture-model"], key_file=key,
                                      base_url="http://localhost:1/v1", output=output, overwrite=False,
                                      checks=["text", "tools", "image"], max_output_tokens=64, timeout=1)
            with patch("probe_model_capabilities.call", side_effect=fake_call), contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(run(args), 1)
            report = output.read_text(encoding="utf-8")
            self.assertNotIn("fixture-secret", report)
            self.assertEqual(calls, ["responses", "chat", "anthropic"])
            self.assertEqual(sum(r["status"] == "skipped" for r in json.loads(report)["records"]), 2)
            with self.assertRaises(ValueError):
                run(args)


if __name__ == "__main__":
    unittest.main()
