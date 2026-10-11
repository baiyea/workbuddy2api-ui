#!/usr/bin/env python3
"""Probe text/tools with anthropic==0.67.0 and httpx==0.28.1.

Default: in-memory responses only. --url enables an explicitly selected local
isolated gateway; neither fixtures nor a mock gateway prove real model support.
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
        if body.get('tools'):
            reply = message(input_tokens, output_tokens)
            reply.update(content=[{'type': 'tool_use', 'id': 'call_fixture', 'name': 'check',
                                   'input': {'q': 'hi'}}], stop_reason='tool_use')
            start = dict(reply, content=[], stop_reason=None, usage={'input_tokens': None, 'output_tokens': None})
            tool_sse = event('message_start', message=start)
            tool_sse += event('content_block_start', index=0, content_block={
                'type': 'tool_use', 'id': 'call_fixture', 'name': 'check', 'input': {}})
            for fragment in ('{"q":', '"hi"}'):
                tool_sse += event('content_block_delta', index=0,
                                  delta={'type': 'input_json_delta', 'partial_json': fragment})
            tool_sse += event('content_block_stop', index=0)
            tool_sse += event('message_delta', delta={'stop_reason': 'tool_use', 'stop_sequence': None},
                              usage=reply['usage']) + event('message_stop')
            if body.get('stream'):
                return httpx.Response(200, headers={'Content-Type': 'text/event-stream'}, content=tool_sse.encode())
            return httpx.Response(200, json=reply)
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


def check_tools(client, model, expected_input, expected_output):
    request = dict(model=model, max_tokens=32,
                   tools=[{'name': 'check', 'input_schema': {'type': 'object',
                           'properties': {'q': {'type': 'string'}}}}],
                   messages=[{'role': 'user', 'content': '你好'},
                             {'role': 'assistant', 'content': [{'type': 'tool_use', 'id': 'call_previous',
                              'name': 'check', 'input': {'q': 'old'}}]},
                             {'role': 'user', 'content': [{'type': 'tool_result',
                              'tool_use_id': 'call_previous', 'content': 'checked'}]}])
    def verify(reply):
        tool = next(block for block in reply.content if block.type == 'tool_use')
        assert (tool.id, tool.name, tool.input) == ('call_fixture', 'check', {'q': 'hi'})
        assert reply.stop_reason == 'tool_use'
        assert_usage(reply.usage, expected_input, expected_output)
    verify(client.messages.create(**request))
    with client.messages.create(stream=True, **request) as raw:
        events = list(raw)
    assert events[-1].type == 'message_stop'
    fragments = [e.delta.partial_json for e in events if e.type == 'content_block_delta'
                 and e.delta.type == 'input_json_delta']
    assert len(fragments) >= 2 and json.loads(''.join(fragments)) == {'q': 'hi'}
    with client.messages.stream(**request) as stream:
        list(stream)
        verify(stream.get_final_message())
    print('  tool JSON/raw deltas/final aggregate/history request: PASS', flush=True)


def check_interleaved(client, model):
    request = dict(model=model, max_tokens=64,
                   messages=[{'role': 'user', 'content': 'fixture-interleaved-claude'}],
                   tools=[{'name': 'check', 'input_schema': {'type': 'object'}}])
    def verify(reply):
        calls = [b for b in reply.content if b.type == 'tool_use']
        assert [(b.id, b.name, b.input) for b in calls] == [
            ('call_a', 'check', {'q': 'hi'}), ('call_b', 'check', {'q': 'bye'})]
        assert ''.join(b.text for b in reply.content if b.type == 'text') == 'interleaved-text-later'
        assert ''.join(b.thinking for b in reply.content if b.type == 'thinking') == 'interleaved-think-later'
        assert reply.stop_reason == 'tool_use'
    verify(client.messages.create(**request))
    with client.messages.stream(**request) as stream:
        events = list(stream)
        result = stream.get_final_message()
    verify(result)
    starts = [e for e in events if e.type == 'content_block_start']
    assert [e.index for e in starts] == list(range(len(starts)))
    assert sum(e.type == 'message_stop' for e in events) == 1
    history = request['messages'] + [{'role': 'assistant', 'content': [b.model_dump(exclude_none=True) for b in result.content]},
        {'role': 'user', 'content': [{'type': 'tool_result', 'tool_use_id': 'call_a', 'content': 'result-a'},
                                    {'type': 'tool_result', 'tool_use_id': 'call_b', 'content': 'result-b'}]}]
    client.messages.create(**dict(request, messages=history))
    print('  parallel/interleaved text/thinking/tool aggregate and generated history: PASS', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", type=local_url)
    parser.add_argument("--key", choices=("fixture-api", "legacy-api"), default="fixture-api")
    parser.add_argument("--model", default="global:mock-model")
    args = parser.parse_args()
    print(f"python={sys.version.split()[0]} anthropic={version('anthropic')} "
          f"httpx={version('httpx')}", flush=True)
    cases = [("local gateway", 1, 1)] if args.url else [
        ("mock unknown→known", 3, 2), ("mock unknown→unknown", None, None),
        ("mock real zero", 0, 0)]
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
                check_tools(client, args.model if args.url else 'cn:mock-model', input_tokens, output_tokens)
                if args.url:
                    check_interleaved(client, args.model)
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
