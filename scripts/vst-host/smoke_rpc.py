"""使用独立临时数据目录验证打包后的服务、VST 宿主与试音文件。"""

import json
import os
import secrets
import subprocess
import tempfile
import urllib.request
import wave
from pathlib import Path


root = Path(__file__).resolve().parents[2]
bundle = root / "artifacts" / "windows-x64"
with tempfile.TemporaryDirectory(prefix="yovoice-vst-smoke-") as data_root:
    token = secrets.token_hex(32)
    env = dict(os.environ, WORKBENCH_DATA=data_root, WORKBENCH_TOKEN=token,
               WORKBENCH_WEB=str(bundle / "web"), WORKBENCH_ENGINE=str(bundle / "engine" / "audiocpp_server.exe"))
    service = subprocess.Popen([str(bundle / "service" / "yovoice-service.exe")], stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env)
    try:
        url = service.stdout.readline().strip()
        if not url.startswith("http://127.0.0.1:"):
            raise RuntimeError(f"服务启动失败：{service.stderr.read()}")

        def call(method, data=None):
            body = json.dumps({"id": "smoke", "method": method, "data": data or {}}).encode()
            request = urllib.request.Request(url + "/api/call", data=body, headers={
                "Content-Type": "application/json", "Cookie": f"vw-{token[:12]}={token}"})
            with urllib.request.urlopen(request, timeout=100) as response:
                result = json.load(response)
            if "error" in result:
                raise RuntimeError(result["error"])
            return result["result"]

        plugins = call("plugin.scan")
        decent = next(item for item in plugins if item["name"].lower().replace(" ", "") == "decentsampler")
        audition = call("plugin.probe", {"path": decent["path"]})
        output = Path(data_root) / "outputs" / audition["fileName"]
        with wave.open(str(output)) as wav:
            assert wav.getnframes() > 0 and wav.getnchannels() == 2
        print(json.dumps({"plugin": audition["name"], "peak": audition["peak"], "bytes": output.stat().st_size}))
    finally:
        if service.poll() is None:
            service.stdin.write("\n")
            service.stdin.flush()
            service.wait(timeout=15)
