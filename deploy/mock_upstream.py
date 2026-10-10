#!/usr/bin/env python3
import json
import argparse
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

requests = []
cancelled = []
state_lock = threading.Lock()


def frame(delta, finish=None, usage=None):
    value = {"id": "mock-1", "object": "chat.completion.chunk", "created": 1,
             "model": "mock-model", "choices": [{"index": 0, "delta": delta}]}
    if finish is not None:
        value["choices"][0]["finish_reason"] = finish
    if usage is not None:
        value["usage"] = usage
    return ('data: ' + json.dumps(value, separators=(',', ':')) + '\n\n').encode()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, _format, *_args):
        pass

    def send_json(self, value):
        body = json.dumps(value, separators=(",", ":")).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/__fixture/state":
            with state_lock:
                self.send_json({"requests": list(requests), "cancelled": list(cancelled)})
            return
        if self.path == "/livez":
            self.send_json({"status": "ok"})
            return
        if self.path == "/v2/enterprises/personal/models":
            self.send_json({"code": 0, "data": {"models": [{"id": "mock-model", "name": "Mock Model", "maxInputTokens": 4096, "maxOutputTokens": 1024}], "agents": [{"name": "cli", "models": ["mock-model"]}]}})
            return
        self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        if self.path != "/console/chat/completions":
            self.send_error(404)
            return
        request = json.loads(body)
        if request.get('model') != 'mock-model':
            self.send_error(404)
            return
        with state_lock:
            requests.append(request)
        # A marker in synthetic content selects a never-ending cancellation fixture.
        slow = 'fixture-slow-' in json.dumps(request.get('messages', []))
        tool = (request.get('tools') or [None])[0]
        delta = {"role": "assistant", "content": "mock-runtime-ok"}
        finish = 'stop'
        if tool:
            function = tool['function']
            arguments = '{"input":"echo fixture"}' if function['name'] == 'exec' else '{"q":"hi"}'
            delta = {"role": "assistant", "tool_calls": [{"index": 0, "id": "call_fixture",
                     "type": "function", "function": {"name": function['name'], "arguments": ""}}]}
            payload = frame(delta) + frame({"tool_calls": [{"index": 0, "function": {"arguments": arguments[:8]}}]})
            payload += frame({"tool_calls": [{"index": 0, "function": {"arguments": arguments[8:]}}]})
            finish = 'tool_calls'
        else:
            payload = frame(delta)
        payload += frame({}, finish, {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2, "credit": 0})
        payload += b'data: [DONE]\n\n'
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        if not slow:
            self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        try:
            if slow:
                for _ in range(400):
                    self.wfile.write(frame({"content": "fixture-streaming"}))
                    self.wfile.flush()
                    time.sleep(.025)
                return
            self.wfile.write(payload)
        except (BrokenPipeError, ConnectionResetError):
            if slow:
                with state_lock:
                    cancelled.append(request.get('conversationId'))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--port', type=int, default=8080)
    args = parser.parse_args()
    ThreadingHTTPServer(("0.0.0.0", args.port), Handler).serve_forever()
