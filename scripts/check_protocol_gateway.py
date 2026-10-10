#!/usr/bin/env python3
"""Assert the isolated console -> core -> mock path, run inside acceptance core.

Targets and keys are fixed synthetic fixtures. No real upstream or credentials.
"""
import http.client
import json
import sys
import threading
import time


def request(path, body=None, session=None, headers=None):
    connection = http.client.HTTPConnection('console', 7863, timeout=8)
    fields = {'Content-Type': 'application/json', 'Origin': sys.argv[1]}
    if session:
        fields.update(Cookie=session[0], **{'X-CSRF-Token': session[1]})
    fields.update(headers or {})
    connection.request('POST' if body is not None else 'GET', path,
                       json.dumps(body).encode() if body is not None else None, fields)
    response = connection.getresponse()
    return connection, response


def post(path, body, session=None, headers=None, status=200):
    connection, response = request(path, body, session, headers)
    try:
        raw = response.read()
        assert response.status == status, (path, response.status, raw)
        return json.loads(raw)
    finally:
        connection.close()


def login():
    connection, response = request('/admin/login', {'key': 'a' * 32})
    try:
        assert response.status == 200
        return response.getheader('Set-Cookie').split(';', 1)[0], json.load(response)['csrf']
    finally:
        connection.close()


def state():
    connection = http.client.HTTPConnection('mock', 8080, timeout=3)
    try:
        connection.request('GET', '/__fixture/state')
        response = connection.getresponse()
        assert response.status == 200
        return json.load(response)
    finally:
        connection.close()


def wait_for(predicate):
    for _ in range(120):
        if predicate():
            return
        time.sleep(.025)
    raise AssertionError('fixture condition did not become true')


def main():
    auth = {'Authorization': 'Bearer legacy-api'}
    schema = {'type': 'object', 'properties': {'answer': {'type': 'string'}},
              'required': ['answer'], 'additionalProperties': False}
    for stream in (False, True):
        body = {'model': 'global:mock-model', 'input': 'fixture-schema', 'stream': stream,
                'text': {'format': {'type': 'json_schema', 'name': 'answer', 'schema': schema, 'strict': True}}}
        connection, response = request('/v1/responses', body, headers=auth)
        try:
            raw = response.read().decode()
            assert response.status == 200, raw
            assert 'mock-runtime-ok' in raw
            assert ('response.completed' in raw) if stream else json.loads(raw)['status'] == 'completed'
        finally:
            connection.close()
        observed = state()['requests'][-1]
        assert observed['model'] == 'mock-model'
        assert observed['response_format'] == {'type': 'json_schema', 'json_schema': {
            'name': 'answer', 'schema': schema, 'strict': True}}, observed
    print('PASS: JSON Schema observed at mock after full Handler, JSON and SSE', flush=True)
    # Unsupported state and authentication fail before the mock receives anything.
    before = len(state()['requests'])
    post('/v1/responses', dict(body, previous_response_id='resp_saved'), headers=auth, status=400)
    post('/v1/responses', body, headers={'Authorization': 'Bearer wrong'}, status=401)
    assert len(state()['requests']) == before

    first, second = login(), login()
    base = {'model': 'global:mock-model', 'max_tokens': 32,
            'messages': [{'role': 'user', 'content': 'hello'}]}
    envelope = {'conversation_id': 'fixture-owner-normal', 'request': base}
    before = len(state()['requests'])
    post('/admin/messages', envelope, status=401)
    post('/admin/messages', envelope, first, headers={'X-CSRF-Token': ''}, status=403)
    post('/admin/messages', envelope, first, headers={'Origin': 'https://invalid.example'}, status=403)
    assert len(state()['requests']) == before
    post('/admin/messages', envelope, first, headers={'X-Console-Owner': 'forged', 'X-Console-Conversation': 'forged'})
    assert state()['requests'][-1]['conversationId'] == envelope['conversation_id']

    # Public private headers cannot create a trusted conversation.
    public_headers = {'x-api-key': 'legacy-api', 'anthropic-version': '2023-06-01',
                      'X-Console-Conversation': 'forged', 'X-Console-Owner': 'forged'}
    post('/v1/messages', base, headers=public_headers)
    assert state()['requests'][-1].get('conversationId') != 'forged'

    streams, failures = {}, []
    def consume(label, session):
        connection = None
        try:
            slow = dict(base, stream=True, messages=[{'role': 'user', 'content': 'fixture-slow-' + label}])
            connection, response = request('/admin/messages', {'conversation_id': label, 'request': slow}, session)
            assert response.status == 200
            while line := response.readline():
                if b'text_delta' in line:
                    streams[label].set()
                assert b'message_stop' not in line, 'cancelled stream declared success'
        except (OSError, http.client.IncompleteRead):
            pass  # An aborted HTTP body is expected after owner logout.
        except Exception as error:
            failures.append(str(error))
        finally:
            if connection:
                connection.close()
    workers = []
    for label, session in [('fixture-owner-a', first), ('fixture-owner-b', second)]:
        streams[label] = threading.Event()
        worker = threading.Thread(target=consume, args=(label, session))
        worker.start()
        workers.append(worker)
        assert streams[label].wait(3), 'stream did not produce a real first delta'
    post('/admin/logout', {}, first)
    workers[0].join(3)
    assert not workers[0].is_alive() and workers[1].is_alive(), 'logout crossed owner boundary'
    wait_for(lambda: 'fixture-owner-a' in state()['cancelled'])
    assert 'fixture-owner-b' not in state()['cancelled']
    post('/admin/logout', {}, second)
    workers[1].join(3)
    wait_for(lambda: 'fixture-owner-b' in state()['cancelled'])
    assert not workers[1].is_alive() and not failures, failures
    print('PASS: management auth/CSRF/origin/trusted ID and separate owner logout cancellation', flush=True)

    connection, response = request('/v1/responses', {
        'model': 'global:mock-model', 'input': 'fixture-slow-disconnect', 'stream': True}, headers=auth)
    line = response.readline()
    while line and b'response.output_text.delta' not in line:
        line = response.readline()
    assert b'response.output_text.delta' in line, 'Responses stream ended before first delta'
    response.close()
    connection.close()
    wait_for(lambda: any('fixture-slow-disconnect' in json.dumps(item['messages'])
                        and item.get('conversationId') in state()['cancelled'] for item in state()['requests']))
    print('PASS: Responses disconnect cancels actual mock upstream', flush=True)


if __name__ == '__main__':
    main()
