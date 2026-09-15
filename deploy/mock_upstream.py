#!/usr/bin/env python3
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


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
        if self.path != "/console/chat/completions" or b'"model":"mock-model"' not in body.replace(b" ", b""):
            self.send_error(404)
            return
        payload = (
            'data: {"id":"mock-1","object":"chat.completion.chunk","created":1,"model":"mock-model","choices":[{"index":0,"delta":{"role":"assistant","content":"mock-runtime-ok"}}]}\n\n'
            'data: {"id":"mock-1","object":"chat.completion.chunk","created":1,"model":"mock-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"credit":0}}\n\n'
            "data: [DONE]\n\n"
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)


ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
