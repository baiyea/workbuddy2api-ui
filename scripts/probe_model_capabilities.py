#!/usr/bin/env python3
"""Opt-in real model probes; standard library only, no retries or tool execution.

Example: WB2A_API_KEY=... python scripts/probe_model_capabilities.py \
  --base-url http://127.0.0.1:7863/v1 --model 'DeepSeek=cn:deepseek-v4.1-flash' \
  --output /path/to/results.json
"""
import argparse
import base64
import datetime as dt
import json
import os
from pathlib import Path
import re
import struct
import sys
import time
import urllib.error
import urllib.request
import uuid
import zlib

CHECKS = ("text", "stream", "history", "tools", "schema", "image", "inline_file")
BODY_LIMIT = 4 << 20


def parse_models(values):
    models, seen = [], set()
    for value in values:
        name, separator, model = value.partition("=")
        if not separator:
            model = name
        name, model = name.strip(), model.strip()
        if not name or not model or model in seen:
            raise ValueError("Models must be unique nonempty NAME=ID pairs (or bare IDs)")
        models.append({"name": name, "id": model})
        seen.add(model)
    return models


def parse_sse(text):
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    frames = text.split("\n\n")
    if frames[-1].strip():
        raise ValueError("Unterminated SSE frame")
    events = []
    for frame in frames[:-1]:
        event, data = "", []
        for line in frame.split("\n"):
            field, colon, value = line.partition(":")
            if value.startswith(" "):
                value = value[1:]
            if field == "event" and colon:
                event = value
            elif field == "data" and colon:
                data.append(value)
        if data:
            raw = "\n".join(data)
            events.append({"event": event, "data": raw if raw == "[DONE]" else json.loads(raw)})
    return events


def summarize(protocol, payload, streaming):
    if protocol == "chat":
        choices = payload.get("choices", [])
        choice = choices[0] if len(choices) == 1 else {}
        message = choice.get("message") or {}
        return {"complete": choice.get("finish_reason") == "stop" and bool(message.get("content")),
                "finish": choice.get("finish_reason"), "text": message.get("content", ""),
                "reasoning_chars": len(message.get("reasoning_content") or ""),
                "usage": payload.get("usage"), "tools": [], "output": [], "errors": []}
    events = payload if streaming else []
    errors = [e["data"] for e in events if isinstance(e["data"], dict) and
              e["data"].get("type") in ("error", "response.failed")]
    usage, blocks, reason, complete = {}, [], None, False
    if protocol == "responses":
        finals = [e["data"] for e in events if isinstance(e["data"], dict) and
                  e["data"].get("type") in ("response.completed", "response.incomplete")]
        obj = finals[-1].get("response", {}) if finals else ({} if streaming else payload)
        reason = obj.get("status")
        complete = reason == "completed" and (not streaming or len(finals) == 1)
        blocks, usage = obj.get("output", []), obj.get("usage")
        text = "".join(part.get("text", "") for b in blocks if b.get("type") == "message"
                       for part in b.get("content", []) if part.get("type") == "output_text")
        thinking = "".join(part.get("text", "") for b in blocks if b.get("type") == "reasoning"
                           for part in b.get("summary", []))
        tools = []
        for b in blocks:
            if b.get("type") == "function_call":
                try:
                    arguments = json.loads(b.get("arguments", ""))
                except (ValueError, TypeError):
                    arguments = b.get("arguments")
                tools.append({"id": b.get("call_id"), "name": b.get("name"), "arguments": arguments})
    else:
        if not streaming:
            blocks, usage, reason = payload.get("content", []), payload.get("usage"), payload.get("stop_reason")
            complete = payload.get("type") == "message" and reason in ("end_turn", "tool_use", "stop_sequence")
        else:
            by_index, arguments, stops = {}, {}, 0
            for event in events:
                obj = event["data"]
                if not isinstance(obj, dict):
                    continue
                kind = obj.get("type")
                if kind == "message_start":
                    usage.update(obj.get("message", {}).get("usage") or {})
                elif kind == "content_block_start":
                    by_index[obj["index"]] = dict(obj["content_block"])
                elif kind == "content_block_delta":
                    index, delta = obj["index"], obj["delta"]
                    block = by_index[index]
                    if delta.get("type") == "text_delta":
                        block["text"] = block.get("text", "") + delta["text"]
                    elif delta.get("type") == "thinking_delta":
                        block["thinking"] = block.get("thinking", "") + delta["thinking"]
                    elif delta.get("type") == "input_json_delta":
                        arguments[index] = arguments.get(index, "") + delta["partial_json"]
                elif kind == "message_delta":
                    reason = obj.get("delta", {}).get("stop_reason", reason)
                    usage.update({k: v for k, v in (obj.get("usage") or {}).items() if v is not None})
                elif kind == "message_stop":
                    stops += 1
            for index, raw in arguments.items():
                try:
                    by_index[index]["input"] = json.loads(raw)
                except ValueError:
                    by_index[index]["input"] = raw
            blocks = [by_index[i] for i in sorted(by_index)]
            complete = stops == 1 and reason in ("end_turn", "tool_use", "stop_sequence")
        text = "".join(b.get("text", "") for b in blocks if b.get("type") == "text")
        thinking = "".join(b.get("thinking", "") for b in blocks if b.get("type") == "thinking")
        tools = [{"id": b.get("id"), "name": b.get("name"), "arguments": b.get("input")}
                 for b in blocks if b.get("type") == "tool_use"]
    return {"complete": complete and not errors, "finish": reason, "text": text,
            "reasoning_chars": len(thinking), "tools": tools, "usage": usage,
            "output": blocks, "errors": errors}


def check_schema(text):
    try:
        value = json.loads(text)
        return (isinstance(value, dict) and set(value) == {"marker", "count"} and
                value["marker"] == "schema_only_47" and type(value["count"]) is int and value["count"] == 7)
    except (ValueError, TypeError):
        return False


def color_image():
    def chunk(name, data):
        return struct.pack("!I", len(data)) + name + data + struct.pack("!I", zlib.crc32(name + data))
    row = b"\xff\x00\x00" * 40 + b"\x00\x00\xff" * 40
    png = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack("!2I5B", 80, 40, 8, 2, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress((b"\x00" + row) * 40)) + chunk(b"IEND", b"")
    return "data:image/png;base64," + base64.b64encode(png).decode()


def call(base, key, protocol, body, timeout):
    endpoint = {"responses": "/v1/responses", "anthropic": "/v1/messages", "chat": "/v1/chat/completions"}[protocol]
    headers = {"Content-Type": "application/json", "Authorization": "Bearer " + key}
    if protocol == "anthropic":
        headers.update({"x-api-key": key, "anthropic-version": "2023-06-01"})
    req = urllib.request.Request(base + endpoint, json.dumps(body).encode(), headers)
    start, first, raw = time.monotonic(), None, bytearray()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as response:
            status = response.status
            content_type = response.headers.get("Content-Type", "")
            while True:
                part = response.read1(8192)
                if not part:
                    break
                first = first or time.monotonic()
                if len(raw) + len(part) > BODY_LIMIT or time.monotonic() - start > timeout:
                    raise ValueError("Probe response byte/time budget exceeded")
                raw.extend(part)
        decoded = raw.decode("utf-8")
        if body.get("stream") and "text/event-stream" not in content_type:
            raise ValueError("Streaming request returned a non-SSE content type")
        payload = parse_sse(decoded) if body.get("stream") else json.loads(decoded)
        summary = summarize(protocol, payload, bool(body.get("stream")))
        return {"http_status": status, "elapsed_ms": round((time.monotonic() - start) * 1000),
                "first_byte_ms": round((first - start) * 1000) if first else None,
                "response": payload, "summary": summary}
    except urllib.error.HTTPError as exc:
        return {"http_status": exc.code, "elapsed_ms": round((time.monotonic() - start) * 1000),
                "error": exc.read(4096).decode("utf-8", errors="replace")}
    except Exception as exc:
        return {"http_status": None, "elapsed_ms": round((time.monotonic() - start) * 1000),
                "error": f"{type(exc).__name__}: {exc}"}


def save_report(report, path):
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)
    lines = ["# 真实模型功能探测", "", "一次探测仅证明该请求的观测结果，不保证模型永久支持或严格遵守 Schema。", "",
             "| 模型 | 协议 | 功能 | 结果 | HTTP | 耗时 |", "|---|---|---|---|---|---|"]
    for r in report["records"]:
        cells = [r["model_name"], r["protocol"], r["check"], r["status"], str(r.get("http_status", "")), str(r.get("elapsed_ms", "")) + " ms"]
        lines.append("| " + " | ".join(str(c).replace("|", "\\|").replace("\n", " ") for c in cells) + " |")
    path.with_suffix(".md").write_text("\n".join(lines) + "\n", encoding="utf-8")


def run(args):
    models = parse_models(args.model)
    key = json.loads(args.key_file.read_text(encoding="utf-8-sig"))["api_key"] if args.key_file else os.environ.get("WB2A_API_KEY")
    if not key:
        raise ValueError("Set WB2A_API_KEY or supply --key-file (JSON api_key)")
    base = args.base_url.rstrip("/")
    if base.endswith("/v1"):
        base = base[:-3]
    if not re.fullmatch(r"https?://[^/?#@]+", base):
        raise ValueError("--base-url must be an HTTP(S) origin, optionally ending /v1")
    if args.output.exists() and not args.overwrite:
        raise ValueError("Output already exists; choose another path or explicitly --overwrite")
    report = {"started_at": dt.datetime.now(dt.timezone.utc).isoformat(), "base_url": base,
              "models": models, "checks": args.checks, "max_output_tokens": args.max_output_tokens,
              "request_retries": 0, "records": [], "limitations": ["Real requests consume account quota.",
              "No tools are executed; tool results are synthetic test data.",
              "not_observed does not establish unsupported; schema success is a single observation.",
              "reasoning_chars observes returned reasoning, not undisclosed model reasoning."]}
    save_report(report, args.output)
    for model in models:
        marker = "probe_" + uuid.uuid4().hex[:10]
        baseline_checked = False

        def probe(protocol, check, body, expected, uncertain=False):
            body = {"model": model["id"], ("max_output_tokens" if protocol == "responses" else "max_tokens"): args.max_output_tokens, **body}
            result = call(base, key, protocol, body, args.timeout)
            summary = result.get("summary", {})
            status = "error" if "error" in result else ("pass" if summary.get("complete") and expected(summary) else ("not_observed" if uncertain and summary.get("complete") else "fail"))
            record = {"model_name": model["name"], "model_id": model["id"], "protocol": protocol,
                      "check": check, "status": status, "request": body, **result}
            report["records"].append(record)
            save_report(report, args.output)
            print(f'{model["name"]} {model["id"]} {protocol}/{check}: {status} HTTP={result.get("http_status")} {result["elapsed_ms"]}ms', flush=True)
            return summary

        for protocol in ("responses", "anthropic"):
            def prompt(text, **extra):
                return {("input" if protocol == "responses" else "messages"):
                        (text if protocol == "responses" else [{"role": "user", "content": text}]), **extra}

            if "text" in args.checks:
                baseline = probe(protocol, "text", prompt("请只输出这个测试标记，不要解释：" + marker), lambda s: s["text"].strip() == marker)
                if not baseline.get("complete"):
                    if not baseline_checked:
                        probe("chat", "diagnostic_baseline", {"messages": [{"role": "user", "content": "只输出 " + marker}]}, lambda s: s["text"].strip() == marker)
                        baseline_checked = True
                    report["records"].append({"model_name": model["name"], "model_id": model["id"], "protocol": protocol,
                                               "check": "remaining", "status": "skipped", "reason": "Text baseline did not complete; avoid further quota consumption"})
                    save_report(report, args.output)
                    continue
            if "stream" in args.checks:
                summary = probe(protocol, "stream", prompt("计算 23 乘 19，只在最终答案输出结果数字。", stream=True), lambda s: "437" in s["text"])
                for check, observed in (("reasoning_observed", bool(summary.get("reasoning_chars"))),
                                        ("usage_observed", any(v is not None for v in (summary.get("usage") or {}).values()))):
                    report["records"].append({"model_name": model["name"], "model_id": model["id"], "protocol": protocol,
                                               "check": check, "status": "pass" if observed else "not_observed", "source_check": "stream"})
                save_report(report, args.output)
            if "history" in args.checks:
                history = [{"role": "user", "content": "请记住测试码 " + marker},
                           {"role": "assistant", "content": "已记住。"}, {"role": "user", "content": "只输出刚才的测试码。"}]
                probe(protocol, "history", {"input" if protocol == "responses" else "messages": history}, lambda s: s["text"].strip() == marker)
            if "tools" in args.checks:
                schema = {"type": "object", "properties": {"value": {"type": "string"}}, "required": ["value"], "additionalProperties": False}
                tool = {"name": "probe_echo", "description": "测试用回显函数；通过它提交给定 value。"}
                tool.update({"type": "function", "parameters": schema} if protocol == "responses" else {"input_schema": schema})
                initial = prompt("必须调用 probe_echo 工具一次，value 参数精确为 " + marker + "。不要自己假装执行。", tools=[tool], stream=True)
                if protocol == "responses":
                    initial["tool_choice"] = {"type": "function", "name": "probe_echo"}
                result = probe(protocol, "tool_call", initial, lambda s: len(s["tools"]) == 1 and s["tools"][0]["name"] == "probe_echo" and bool(s["tools"][0]["id"]) and s["tools"][0]["arguments"] == {"value": marker}, uncertain=True)
                if (len(result.get("tools", [])) == 1 and result["tools"][0].get("id") and
                        result["tools"][0].get("name") == "probe_echo" and result["tools"][0].get("arguments") == {"value": marker}):
                    reply = "result_" + uuid.uuid4().hex[:10]
                    if protocol == "responses":
                        history = [{"role": "user", "content": initial["input"]}, *result["output"],
                                   {"type": "function_call_output", "call_id": result["tools"][0]["id"], "output": reply},
                                   {"role": "user", "content": "只输出刚才工具返回的结果，不要再调用工具。"}]
                        body = {"input": history, "tools": [tool], "tool_choice": "none"}
                    else:
                        history = [*initial["messages"], {"role": "assistant", "content": result["output"]},
                                   {"role": "user", "content": [{"type": "tool_result", "tool_use_id": result["tools"][0]["id"], "content": reply},
                                                                {"type": "text", "text": "只输出刚才工具返回的结果，不要再调用工具。"}]}]
                        body = {"messages": history, "tools": [tool]}
                    probe(protocol, "tool_result", body, lambda s: s["text"].strip() == reply)
            if protocol == "responses":
                if "schema" in args.checks:
                    schema = {"type": "object", "properties": {"marker": {"type": "string", "const": "schema_only_47"}, "count": {"type": "integer", "const": 7}}, "required": ["marker", "count"], "additionalProperties": False}
                    probe(protocol, "strict_schema_conflict", prompt("请用普通中文写一句测试说明，不要输出 JSON。", text={"format": {"type": "json_schema", "name": "probe_schema", "strict": True, "schema": schema}}), lambda s: check_schema(s["text"]))
                if "image" in args.checks:
                    body = {"input": [{"role": "user", "content": [{"type": "input_text", "text": "图片中左半和右半分别是什么颜色？按左到右只输出两个英文颜色词，以逗号分隔。"}, {"type": "input_image", "image_url": color_image(), "detail": "low"}]}]}
                    probe(protocol, "image", body, lambda s: s["text"].strip().lower().replace(" ", "").rstrip(".") == "red,blue")
                if "inline_file" in args.checks:
                    data = base64.b64encode(("test_value=" + marker).encode()).decode()
                    body = {"input": [{"role": "user", "content": [{"type": "input_text", "text": "只输出附件中的 test_value 值。"}, {"type": "input_file", "filename": "probe.txt", "file_data": "data:text/plain;base64," + data}]}]}
                    probe(protocol, "inline_text_file", body, lambda s: s["text"].strip() == marker)
            else:
                if "schema" in args.checks:
                    report["records"].append({"model_name": model["name"], "model_id": model["id"], "protocol": protocol,
                                               "check": "strict_schema_conflict", "status": "skipped",
                                               "reason": "This project's Messages route rejects output_format; not a model capability result"})
                    save_report(report, args.output)
                if "image" in args.checks:
                    content = [{"type": "text", "text": "图片中左半和右半分别是什么颜色？按左到右只输出两个英文颜色词，以逗号分隔。"},
                               {"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": color_image().split(",", 1)[1]}}]
                    probe(protocol, "image", {"messages": [{"role": "user", "content": content}]}, lambda s: s["text"].strip().lower().replace(" ", "").rstrip(".") == "red,blue")
                if "inline_file" in args.checks:
                    content = [{"type": "text", "text": "只输出附件中的 test_value 值。"},
                               {"type": "document", "source": {"type": "text", "media_type": "text/plain", "data": "test_value=" + marker}}]
                    probe(protocol, "inline_text_file", {"messages": [{"role": "user", "content": content}]}, lambda s: s["text"].strip() == marker)
    report["completed_at"] = dt.datetime.now(dt.timezone.utc).isoformat()
    save_report(report, args.output)
    return int(any(record["status"] in ("error", "fail") for record in report["records"]))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--model", action="append", required=True, help="Repeat NAME=MODEL_ID, or a bare model ID")
    parser.add_argument("--key-file", type=Path, help="Read api_key from local JSON; otherwise use WB2A_API_KEY")
    parser.add_argument("--output", type=Path, required=True, help="Writes JSON evidence and sibling Markdown table")
    parser.add_argument("--checks", nargs="+", choices=CHECKS, default=list(CHECKS))
    parser.add_argument("--timeout", type=float, default=150)
    parser.add_argument("--max-output-tokens", type=int, default=1536)
    parser.add_argument("--overwrite", action="store_true")
    args = parser.parse_args()
    if args.timeout <= 0 or args.max_output_tokens <= 0:
        parser.error("Timeout and max output tokens must be positive")
    return run(args)


if __name__ == "__main__":
    sys.exit(main())
