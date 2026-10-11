import argparse
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from probe_model_capabilities import parse_models, parse_sse, summarize, check_schema, run


class ProbeTests(unittest.TestCase):
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
