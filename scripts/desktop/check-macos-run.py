import os
from pathlib import Path
import shutil
import subprocess
import tempfile


# 模拟系统启动器，验证应用以 bundle 启动以及日志转发和清理。
with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    scripts = root / "scripts/desktop"
    scripts.mkdir(parents=True)
    runner = scripts / "run-macos.sh"
    shutil.copy(Path(__file__).with_name("run-macos.sh"), runner)
    binary = root / "bin"
    binary.mkdir()
    launcher = binary / "open"
    launcher.write_text('''#!/bin/bash
[[ "$1" == -W && "$2" == --stdout && "$4" == --stderr ]] || exit 90
[[ "${!#}" == "$PWD/artifacts/macos-arm64/yovoice.app" ]] || exit 91
[[ "$6" == --env && "$7" == "WORKBENCH_DATA=$WORKBENCH_DATA" ]] || exit 92
echo host-output >> "$3"
echo service-output >> "$WORKBENCH_DATA/logs/host-service.log"
echo engine-output >> "$WORKBENCH_DATA/logs/engine.log"
sleep 2
exit 7
''')
    launcher.chmod(0o755)
    result = subprocess.run(
        ["bash", str(runner)], capture_output=True, text=True, timeout=10,
        env={**os.environ, "PATH": str(binary) + os.pathsep + os.environ["PATH"], "WORKBENCH_DATA": str(root / "data")},
    )
    assert result.returncode == 7, result
    for message in ("host-output", "service-output", "engine-output"):
        assert message in result.stdout, result.stdout
print("前台输出、服务日志、引擎日志和退出清理通过。")
