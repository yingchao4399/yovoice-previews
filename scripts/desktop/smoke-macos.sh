#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/../.."
app="$PWD/artifacts/macos-arm64/yovoice.app"
result=$(mktemp -d /tmp/voice-workbench-smoke.XXXXXX)
WORKBENCH_DATA="$result" WORKBENCH_SMOKE_SCRIPT="$PWD/scripts/desktop/smoke-macos.js" "$app/Contents/MacOS/VoiceWorkbenchMac" &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT
for ((i=0;i<60;i++)); do
  if ! kill -0 "$pid" 2>/dev/null; then break; fi
  sleep 1
done
if kill -0 "$pid" 2>/dev/null; then echo '原生测试或退出超时'; exit 1; fi
wait "$pid"
if [[ -f "$result/smoke-error.txt" ]]; then cat "$result/smoke-error.txt"; exit 1; fi
python3 - "$result" <<'PY'
import json,pathlib,sqlite3,sys
root=pathlib.Path(sys.argv[1])
result=json.loads((root/'smoke-result.json').read_text())
db=sqlite3.connect(root/'yovoice.db')
draft=json.loads(db.execute('SELECT data FROM drafts ORDER BY pos LIMIT 1').fetchone()[0])
assert result['desktop'] and result['platform']=='macos' and result['audioSeconds']==2
assert draft['text']==result['draft']
print('原生桥接、音频、草稿保存和退出通过。截图：'+str(root/'smoke.png'))
PY
