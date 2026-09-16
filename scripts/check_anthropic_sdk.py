#!/usr/bin/env python3
"""Probe the approved text contract with anthropic==0.67.0 and httpx==0.28.1.

Default: in-memory responses only. --url enables an explicitly selected local
fixture gateway; a passing mock probe does not prove gateway compatibility.
Use Python 3.12 and install the pinned SDKs in a temporary venv, never in a
production image; SDK 0.67.0 fails raw SSE type parsing under Python 3.14.
"""
import argparse
import json
import sys
from importlib.metadata import version
from urllib.parse import urlsplit

import httpx
from anthropic import Anthropic


def message(input_tokens, output_tokens):
    return {
        "id": "msg_fixture", "type": "message", "role": "assistant",
        "model": "cn:mock-model", "content": [{"type": "text", "text": "你好"}],
        "stop_reason": "end_turn", "stop_sequence": None,
        "usage": {"input_tokens": input_tokens, "output_tokens": output_tokens},
    }


def event(kind, **fields):
    return "event: " + kind + "\ndata: " + json.dumps(
        {"type": kind, **fields}, ensure_ascii=False) + "\n\n"


def mock_transport(input_tokens, output_tokens):
    start = message(None, None)
    start.update(content=[], stop_reason=None)
    sse = (
        event("message_start", message=start)
        + event("content_block_start", index=0, content_block={"type": "text", "text": ""})
        + event("content_block_delta", index=0, delta={"type": "text_delta", "text": "你"})
        + event("content_block_delta", index=0, delta={"type": "text_delta", "text": "好"})
        + event("content_block_stop", index=0)
        + event("message_delta", delta={"stop_reason": "end_turn", "stop_sequence": None},
                usage={"input_tokens": input_tokens, "output_tokens": output_tokens})
        + event("message_stop")
    )

    def respond(request):
        assert request.method == "POST"
        assert request.url.path == "/v1/messages"
        assert request.headers["anthropic-version"] == "2023-06-01"
        assert request.headers["x-api-key"] == "fixture-api"
        assert "authorization" not in request.headers
        body = json.loads(request.content)
        assert body["model"] == "cn:mock-model" and body["max_tokens"] == 32
        assert body["messages"] == [{"role": "user", "content": "你好"}]
        if body.get("stream"):
            return httpx.Response(200, headers={"Content-Type": "text/event-stream"},
                                  content=sse.encode())
        return httpx.Response(200, json=message(input_tokens, output_tokens))

    return httpx.MockTransport(respond)


def assert_usage(usage, expected_input, expected_output):
    assert usage.output_tokens == expected_output, (
        f"output_tokens: expected {expected_output!r}, got {usage.output_tokens!r}")
    assert usage.input_tokens == expected_input, (
        f"input_tokens: expected {expected_input!r}, got {usage.input_tokens!r}")


def check(client, model, expected_text, expected_input, expected_output):
    request = dict(model=model, max_tokens=32,
                   messages=[{"role": "user", "content": "你好"}])
    reply = client.messages.create(**request)
    assert reply.content[0].text == expected_text
    assert reply.stop_reason == "end_turn"
    assert_usage(reply.usage, expected_input, expected_output)
    print("  ordinary JSON: PASS", flush=True)

    with client.messages.create(stream=True, **request) as raw:
        events = list(raw)
    kinds = [item.type for item in events]
    assert kinds[:2] == ["message_start", "content_block_start"], kinds
    assert kinds[-3:] == ["content_block_stop", "message_delta", "message_stop"], kinds
    assert all(kind == "content_block_delta" for kind in kinds[2:-3]), kinds
    assert "".join(item.delta.text for item in events[2:-3]) == expected_text
    assert events[0].message.content == []
    assert events[0].message.stop_reason is None
    assert_usage(events[0].message.usage, None, None)
    assert events[-2].delta.stop_reason == "end_turn"
    assert_usage(events[-2].usage, expected_input, expected_output)
    print("  raw SSE: PASS", flush=True)

    with client.messages.stream(**request) as stream:
        assert "".join(stream.text_stream) == expected_text
        final = stream.get_final_message()
    print(f"  aggregate: input={final.usage.input_tokens!r}, "
          f"output={final.usage.output_tokens!r}", flush=True)
    assert final.content[0].text == expected_text
    assert final.stop_reason == "end_turn"
    assert_usage(final.usage, expected_input, expected_output)
    print("  aggregate: PASS", flush=True)


def local_url(value):
    try:
        parsed = urlsplit(value)
        if (parsed.scheme != "http" or parsed.hostname not in ("localhost", "127.0.0.1")
                or parsed.username is not None or parsed.password is not None
                or parsed.path not in ("", "/") or parsed.query or parsed.fragment
                or parsed.port == 0):
            raise ValueError
    except ValueError:
        raise argparse.ArgumentTypeError("URL must be an HTTP localhost/127.0.0.1 root URL") from None
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", type=local_url)
    parser.add_argument("--key", choices=("fixture-api", "legacy-api"), default="fixture-api")
    parser.add_argument("--model", default="global:mock-model")
    args = parser.parse_args()
    print(f"python={sys.version.split()[0]} anthropic={version('anthropic')} "
          f"httpx={version('httpx')}", flush=True)
    cases = [("local gateway", 1, 1)] if args.url else [
        ("mock unknown→known", 3, 2), ("mock unknown→unknown", None, None)]
    failures = []
    for label, input_tokens, output_tokens in cases:
        print(label, flush=True)
        transport = None if args.url else mock_transport(input_tokens, output_tokens)
        # No ambient auth token, proxy, or redirects may escape the fixture target.
        with Anthropic(api_key=args.key if args.url else "fixture-api", auth_token="",
                       base_url=args.url or "http://localhost", max_retries=0,
                       http_client=httpx.Client(transport=transport, trust_env=False,
                                                follow_redirects=False, timeout=10)) as client:
            # Constructor None would import the environment token; remove the
            # explicit empty token afterward so no empty Bearer header is sent.
            client.auth_token = None
            try:
                check(client, args.model if args.url else "cn:mock-model",
                      "mock-runtime-ok" if args.url else "你好", input_tokens, output_tokens)
            except AssertionError as error:
                failures.append(f"{label}: {error}")
    if failures:
        for failure in failures:
            print("FAIL: " + failure, file=sys.stderr)
        return 1
    print("PASS: " + ("isolated gateway" if args.url else "in-memory SDK contract only"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
