import json
from pathlib import Path
import subprocess
import socket
import sys
import time
import unittest
import urllib.request


class MockUpstreamTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            cls.url = 'http://127.0.0.1:' + str(listener.getsockname()[1])
            port = str(listener.getsockname()[1])
        cls.process = subprocess.Popen([sys.executable, str(Path(__file__).with_name('mock_upstream.py')), '--port', port],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        cls.client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        # HTTPServer resolves its bind address; isolated Docker DNS can be slow.
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            try:
                if cls.process.poll() is not None:
                    raise RuntimeError(cls.process.communicate()[1].decode())
                cls.client.open(cls.url + '/livez', timeout=1).close()
                return
            except OSError:
                time.sleep(.03)
        cls.process.terminate()
        stderr = cls.process.communicate(timeout=5)[1].decode()
        raise RuntimeError('mock failed to start: ' + stderr)

    @classmethod
    def tearDownClass(cls):
        cls.process.terminate()
        cls.process.communicate(timeout=5)

    def test_tools_and_observed_schema(self):
        schema = {'type': 'json_schema', 'json_schema': {'name': 'answer', 'strict': True,
                  'schema': {'type': 'object', 'properties': {'answer': {'type': 'string'}}}}}
        body = {'model': 'mock-model', 'messages': [{'role': 'user', 'content': 'hello'}],
                'tools': [{'type': 'function', 'function': {'name': 'check'}}],
                'response_format': schema}
        request = urllib.request.Request(self.url + '/console/chat/completions',
                                         data=json.dumps(body).encode())
        with self.client.open(request, timeout=2) as response:
            frames = response.read().decode()
        self.assertIn('"tool_calls"', frames)
        self.assertIn('call_fixture', frames)
        with self.client.open(self.url + '/__fixture/state', timeout=2) as response:
            state = json.load(response)
        self.assertEqual(state['requests'][-1]['response_format'], schema)

    def test_parallel_tools_interleave_text_and_reasoning(self):
        body = {'model': 'mock-model', 'messages': [{'role': 'user', 'content': 'fixture-interleaved'}],
                'tools': [{'type': 'function', 'function': {'name': 'check'}}]}
        request = urllib.request.Request(self.url + '/console/chat/completions', data=json.dumps(body).encode())
        with self.client.open(request, timeout=2) as response:
            raw = response.read().decode()
        frames = [json.loads(line[6:]) for line in raw.splitlines() if line.startswith('data: {')]
        calls = {}
        for frame in frames:
            for call in frame['choices'][0]['delta'].get('tool_calls', []):
                calls.setdefault(call['index'], '')
                calls[call['index']] += call['function'].get('arguments', '')
        self.assertEqual(calls, {0: '{"q":"hi"}', 1: '{"q":"bye"}'})
        self.assertIn('interleaved-text', raw)
        self.assertIn('interleaved-think', raw)


if __name__ == '__main__':
    unittest.main()
