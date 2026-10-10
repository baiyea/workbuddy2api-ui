#!/usr/bin/env python3
"""Isolated gateway Responses SDK checks; Python3.12, openai==2.6.1/httpx==0.28.1.

No SDK is a production dependency. --url accepts only a local fixture gateway.
The mock's generated text is intentionally not checked against a JSON Schema.
"""
import argparse
import json
import sys
from importlib.metadata import version

import httpx
from openai import OpenAI

from check_anthropic_sdk import local_url


def check(client, model):
    request = dict(model=model, input='hello', max_output_tokens=32, store=False)
    reply = client.responses.create(**request)
    assert reply.object == 'response' and reply.status == 'completed'
    assert reply.output_text == 'mock-runtime-ok'
    assert (reply.usage.input_tokens, reply.usage.output_tokens, reply.usage.total_tokens) == (1, 1, 2)
    with client.responses.create(stream=True, **request) as raw:
        events = list(raw)
    assert events[0].type == 'response.created' and events[-1].type == 'response.completed'
    assert [e.sequence_number for e in events] == list(range(len(events)))
    assert ''.join(e.delta for e in events if e.type == 'response.output_text.delta') == reply.output_text
    print('  raw text events: ' + ', '.join(e.type for e in events), flush=True)
    with client.responses.stream(**request) as stream:
        list(stream)
        final = stream.get_final_response()
    assert final.output_text == reply.output_text and final.status == 'completed'
    assert final.usage == reply.usage
    print('  create/raw events/final aggregate/usage: PASS', flush=True)

    for kind, name, expected in [('function', 'check', '{"q":"hi"}'), ('custom', 'exec', 'echo fixture')]:
        tool = {'type': kind, 'name': name}
        if kind == 'function':
            tool['parameters'] = {'type': 'object', 'properties': {'q': {'type': 'string'}}}
        args = dict(model=model, input='use tool', tools=[tool], store=False)
        def verify(response):
            call = next(item for item in response.output if item.type == kind + '_tool_call'
                        or item.type == 'function_call')
            assert call.type == ('custom_tool_call' if kind == 'custom' else 'function_call')
            assert (call.call_id, call.name) == ('call_fixture', name)
            assert getattr(call, 'input' if kind == 'custom' else 'arguments') == expected
        result = client.responses.create(**args)
        verify(result)
        with client.responses.create(stream=True, **args) as raw:
            events = list(raw)
        prefix = 'response.custom_tool_call_input' if kind == 'custom' else 'response.function_call_arguments'
        fragments = [e.delta for e in events if e.type == prefix + '.delta']
        assert fragments and ''.join(fragments) == expected
        assert [e.sequence_number for e in events] == list(range(len(events)))
        verify(events[-1].response)
        with client.responses.stream(**args) as stream:
            list(stream)
            verify(stream.get_final_response())
        output = {'type': 'custom_tool_call_output' if kind == 'custom' else 'function_call_output',
                  'call_id': 'call_fixture', 'output': 'checked'}
        # Pass the gateway's own output back as full history; no stored response ID.
        client.responses.create(**dict(args, input=[result.output[0].model_dump(exclude_none=True), output]))
        print(f'  {kind} ID/arguments/raw deltas/final aggregate/history: PASS', flush=True)

    schema = {'type': 'object', 'properties': {'answer': {'type': 'string'}},
              'required': ['answer'], 'additionalProperties': False}
    client.responses.create(**request, text={'format': {'type': 'json_schema', 'name': 'answer',
                            'schema': schema, 'strict': True}})
    print('  schema request accepted (upstream observation checked separately): PASS', flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--url', type=local_url, required=True)
    parser.add_argument('--key', choices=('fixture-api', 'legacy-api'), default='fixture-api')
    parser.add_argument('--model', default='global:mock-model')
    args = parser.parse_args()
    print(f'python={sys.version.split()[0]} openai={version("openai")} httpx={version("httpx")}', flush=True)
    with OpenAI(api_key=args.key, base_url=args.url.rstrip('/') + '/v1', max_retries=0,
                http_client=httpx.Client(trust_env=False, follow_redirects=False, timeout=10)) as client:
        check(client, args.model)
    print('PASS: isolated gateway SDK; no real upstream evidence')


if __name__ == '__main__':
    main()
