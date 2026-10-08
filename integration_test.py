#!/usr/bin/env python3
"""Test API-only installation and enforcement in a local CPA v8.0.4 process.

The upstream server, all keys, and all model requests are local test fixtures.
No production configuration or upstream account is used.
"""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

from package import ARCHIVE_NAME, make_release

ROOT = Path(__file__).resolve().parent
BLOCKED = "local-test-blocked-client"
ALLOWED = "local-test-allowed-client"
MANAGEMENT = "local-test-management"
scope = hashlib.sha256(b"cli-proxy-api:caller-scope:v1\x00" + BLOCKED.encode()).hexdigest()
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def request(base, route, data=None, headers=None, method=None):
    req = urllib.request.Request(base + route, data=json.dumps(data).encode() if data is not None else None,
                                 headers={"Content-Type": "application/json", **(headers or {})}, method=method)
    try:
        with opener.open(req, timeout=30) as r:
            return r.status, r.read()
    except urllib.error.HTTPError as e:
        return e.code, e.read()


def run(binary):
    with tempfile.TemporaryDirectory(prefix="key-chat-access-test-") as work:
        work = Path(work)
        upstream_calls = []

        class Mock(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def do_GET(self):
                p = work / "release" / self.path.lstrip("/")
                if p.name not in ("registry.json", ARCHIVE_NAME) or not p.exists():
                    self.send_error(404)
                    return
                data = p.read_bytes()
                self.send_response(200)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def do_POST(self):
                body = self.rfile.read(int(self.headers["Content-Length"]))
                upstream_calls.append((self.path, body))
                data = json.dumps({
                    "id": "chatcmpl-local-test", "object": "chat.completion", "created": 1,
                    "model": "test-chat", "choices": [{"index": 0, "finish_reason": "stop",
                    "message": {"role": "assistant", "content": "ok"}}],
                    "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
                }).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

        mock = ThreadingHTTPServer(("127.0.0.1", 0), Mock)
        mock_base = "http://127.0.0.1:" + str(mock.server_port)
        make_release(mock_base, work / "release")
        threading.Thread(target=mock.serve_forever, daemon=True).start()
        # Obtain a local free port for the host.
        import socket
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            port = s.getsockname()[1]
        base = "http://127.0.0.1:" + str(port)
        registry_url = mock_base + "/registry.json"
        cfg = {
            "config-version": 8,
            "server": {"host": "127.0.0.1", "port": port},
            "management": {"secret-key": MANAGEMENT, "disable-control-panel": True, "disable-auto-update-panel": True},
            "access": {"api-keys": [BLOCKED, ALLOWED]},
            "oauth": {"auth-dir": str(work / "auths")},
            "requests": {"proxy-url": "direct"},
            "api-keys": {"openai-compatibility": [{
                "name": "local-mock", "base-url": mock_base + "/v1",
                "keys": [{"api-key": "local-test-upstream"}],
                "models": [{"name": "test-chat", "alias": "test-chat"}],
            }]},
            "plugins": {"enabled": True, "dir": str(work / "plugins"), "store-sources": [registry_url],
                        "store-auth": [{"match": mock_base + "/", "type": "none", "allow-insecure": True}],
                        "configs": {"key-chat-access": {"enabled": True, "priority": 1000,
                                                           "blocked_caller_scopes": [scope]}}},
        }
        (work / "config.yaml").write_text(json.dumps(cfg))  # JSON is valid YAML.
        management = {"Authorization": "Bearer " + MANAGEMENT}
        with (work / "host.log").open("w") as log:
            host = subprocess.Popen([str(binary), "-config", str(work / "config.yaml"), "-local-model"],
                                    cwd=work, stdout=log, stderr=subprocess.STDOUT)
            try:
                for _ in range(100):
                    if host.poll() is not None:
                        raise RuntimeError("CPA exited during startup")
                    try:
                        if request(base, "/healthz")[0] == 200:
                            break
                    except OSError:
                        pass
                    time.sleep(.1)
                else:
                    raise RuntimeError("CPA startup timeout")

                source_id = "source-" + hashlib.sha256(registry_url.encode()).hexdigest()[:12]
                status, raw = request(base, "/v8/management/plugins/store/key-chat-access/install?source=" + source_id,
                                      {}, management)
                assert status == 200, ("API installation failed", status, raw.decode())
                for _ in range(100):
                    _, raw = request(base, "/v8/management/plugins", headers=management)
                    entries = json.loads(raw).get("plugins", [])
                    item = next((p for p in entries if p["id"] == "key-chat-access"), {})
                    if item.get("registered"):
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError("Installed plugin did not register: " + raw.decode())
                print("PASS: plugin installed and loaded using only Management API")

                chat = {"model": "test-chat", "messages": [{"role": "user", "content": "hi"}]}
                cases = [
                    ("nonstream", "/v1/chat/completions", {"Authorization": "Bearer " + BLOCKED}, chat),
                    ("stream", "/v1/chat/completions", {"Authorization": "Bearer " + BLOCKED}, {**chat, "stream": True}),
                    ("X-Api-Key", "/v1/chat/completions", {"X-Api-Key": BLOCKED}, chat),
                    ("X-Goog-Api-Key", "/v1/chat/completions", {"X-Goog-Api-Key": BLOCKED}, chat),
                    ("query key", "/v1/chat/completions?key=" + BLOCKED, {}, chat),
                    ("query auth_token", "/v1/chat/completions?auth_token=" + BLOCKED, {}, chat),
                    ("spoofed identity", "/v1/chat/completions", {"Authorization": "Bearer " + BLOCKED,
                     "X-Caller-Scope": "spoofed"}, {**chat, "metadata": {"caller_scope": "spoofed", "request_path": "/v1/responses"}}),
                    ("large body", "/v1/chat/completions", {"Authorization": "Bearer " + BLOCKED},
                     {"model": "test-chat", "messages": [{"role": "user", "content": "x" * (8 * 1024 * 1024)}]}),
                ]
                for name, route, headers, body in cases:
                    count = len(upstream_calls)
                    status, raw = request(base, route, body, headers)
                    assert status == 403, (name, status, raw[:500])
                    assert json.loads(raw)["error"]["code"] == "chat_completions_disabled", (name, raw)
                    assert len(upstream_calls) == count, name + " reached upstream"
                    print("PASS: blocked " + name + ", zero upstream calls")

                for name, route, key, body in [
                    ("another user", "/v1/chat/completions", ALLOWED, chat),
                    ("Responses", "/v1/responses", BLOCKED, {"model": "test-chat", "input": "hi"}),
                    ("Messages", "/v1/messages", BLOCKED, {**chat, "max_tokens": 1}),
                    ("legacy Completions", "/v1/completions", BLOCKED, {"model": "test-chat", "prompt": "hi"}),
                ]:
                    count = len(upstream_calls)
                    status, raw = request(base, route, body, {"Authorization": "Bearer " + key})
                    assert status == 200, (name, status, raw[:500])
                    assert len(upstream_calls) == count + 1, name + " did not reach mock upstream"
                    print("PASS: allowed " + name)

                status, _ = request(base, "/v1/models", headers={"Authorization": "Bearer " + BLOCKED})
                assert status == 200, ("models", status)
                print("PASS: allowed model listing")

                # Verify the actual management-panel toggle releases and restores the rule.
                node = "/v8/management/config/plugins/configs/key-chat-access/enabled"
                for flag, expected in [(False, 200), (True, 403)]:
                    status, raw = request(base, node, flag, management, "PUT")
                    assert status == 200, ("toggle", flag, status, raw)
                    for _ in range(100):
                        status, raw = request(base, "/v1/chat/completions", chat, {"Authorization": "Bearer " + BLOCKED})
                        if status == expected:
                            break
                        time.sleep(.1)
                    assert status == expected, ("toggle enforcement", flag, status, raw)
                print("PASS: management enable/disable toggles hot-reload without SSH or restart")
            except Exception:
                log.flush()
                print((work / "host.log").read_text()[-18000:])
                raise
            finally:
                host.terminate()
                try:
                    host.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    host.kill()
                    host.wait()
                mock.shutdown()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--cpa", required=True, type=Path)
    args = parser.parse_args()
    run(args.cpa.resolve())
