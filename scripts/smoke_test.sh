#!/usr/bin/env bash
#
# smoke_test.sh —— 真实启动服务、请求健康检查、完成一次事件接入与投递查询，
# 并以 trap 清理服务进程与临时数据。失败时以非零码退出。
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT_DIR/bin/router-smoke"
TMP_DIR="$(mktemp -d)"
DB_PATH="$TMP_DIR/router.db"
PORT="${SMOKE_PORT:-18080}"
BASE_URL="http://127.0.0.1:$PORT"

SERVER_PID=""
TARGET_PID=""
TARGET_PORT="${SMOKE_TARGET_PORT:-18081}"

cleanup() {
  # 停止服务与目标接收进程
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  [[ -n "$TARGET_PID" ]] && kill "$TARGET_PID" 2>/dev/null || true
  wait "$SERVER_PID" 2>/dev/null || true
  wait "$TARGET_PID" 2>/dev/null || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

log() { echo "[smoke] $*"; }

# 1. 编译服务二进制
log "building server binary"
mkdir -p "$ROOT_DIR/bin"
CGO_ENABLED=0 go build -o "$BIN" "$ROOT_DIR/cmd/server" || fail "build failed"

# 2. 启动目标接收服务（返回 200，用于验证转发成功）
if command -v python3 >/dev/null 2>&1; then
  log "starting target receiver on :$TARGET_PORT"
  python3 - "$TARGET_PORT" <<'PY' &
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port = int(sys.argv[1])

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get('Content-Length', 0))
        self.rfile.read(length)
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')

    def log_message(self, *args):
        pass

HTTPServer(('127.0.0.1', port), Handler).serve_forever()
PY
  TARGET_PID=$!
  sleep 0.5
else
  log "python3 not found; using blackhole target (connection refused)"
fi

# 3. 启动服务
log "starting service on :$PORT"
DB_PATH="$DB_PATH" \
  HTTP_ADDR=":$PORT" \
  MAX_RETRIES=5 \
  RETRY_BASE_MS=50 \
  FORWARD_TIMEOUT_MS=2000 \
  ALLOW_PRIVATE_TARGET=true \
  ALLOW_LOOPBACK_TARGET=true \
  "$BIN" >"$TMP_DIR/server.log" 2>&1 &
SERVER_PID=$!

# 4. 等待健康检查通过
log "waiting for healthz"
health_ok=0
for i in $(seq 1 40); do
  if curl -fsS "$BASE_URL/healthz" >/dev/null 2>&1; then
    health_ok=1
    break
  fi
  sleep 0.25
done
[[ "$health_ok" -eq 1 ]] || { cat "$TMP_DIR/server.log" >&2; fail "healthz did not become healthy"; }

status="$(curl -fsS "$BASE_URL/healthz")"
echo "$status" | grep -q '"ok"' || fail "healthz body missing ok"
log "healthz OK: $status"

# 5. 创建来源
SECRET="smoke-secret"
log "creating source"
SRC_JSON="$(curl -fsS -X POST "$BASE_URL/api/v1/sources" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"smoke-src\",\"secret\":\"$SECRET\",\"allowed_event_types\":[\"push\"]}")"
SRC_ID="$(echo "$SRC_JSON" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])' 2>/dev/null || echo "$SRC_JSON" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)"
[[ -n "$SRC_ID" ]] || fail "failed to parse source id from: $SRC_JSON"

# 6. 创建目标（指向接收服务或黑洞）
TARGET_URL="http://127.0.0.1:$TARGET_PORT/hook"
log "creating target -> $TARGET_URL"
TGT_JSON="$(curl -fsS -X POST "$BASE_URL/api/v1/targets" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"smoke-tgt\",\"url\":\"$TARGET_URL\"}")"
TGT_ID="$(echo "$TGT_JSON" | python3 -c 'import sys,json;print(json.load(sys.stdin)["id"])' 2>/dev/null || echo "$TGT_JSON" | grep -o '"id":[0-9]*' | head -1 | cut -d: -f2)"
[[ -n "$TGT_ID" ]] || fail "failed to parse target id from: $TGT_JSON"

# 7. 创建规则
log "creating rule"
curl -fsS -X POST "$BASE_URL/api/v1/rules" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"smoke-rule\",\"source_id\":$SRC_ID,\"event_type\":\"push\",\"target_id\":$TGT_ID}" >/dev/null \
  || fail "rule creation failed"

# 8. 接入事件（计算 HMAC-SHA256 签名）
EVENT_BODY='{"source_id":'"$SRC_ID"',"event_type":"push","event_id":"smoke-1","payload":{"repo":"smoke"}}'
SIGNATURE="$(printf '%s' "$EVENT_BODY" | openssl dgst -sha256 -hmac "$SECRET" -hex | awk '{print $NF}')"
log "posting event"
EVENT_RESP="$(curl -fsS -X POST "$BASE_URL/api/v1/events" \
  -H 'Content-Type: application/json' \
  -H "X-Signature: $SIGNATURE" \
  -d "$EVENT_BODY")"
echo "$EVENT_RESP" | grep -q '"accepted"' || fail "event not accepted: $EVENT_RESP"
log "event accepted: $EVENT_RESP"

# 9. 稍候转发完成
sleep 1

# 10. 查询投递记录
log "querying deliveries"
DELIV="$(curl -fsS "$BASE_URL/api/v1/deliveries?limit=10")"
echo "$DELIV" | grep -q '"deliveries"' || fail "deliveries query failed: $DELIV"
DELIV_COUNT="$(echo "$DELIV" | grep -o '"attempts":[0-9]*' | wc -l | tr -d ' ')"
[[ "$DELIV_COUNT" -ge 1 ]] || fail "expected at least one delivery, got: $DELIV"
log "delivery recorded: $DELIV"

log "SMOKE OK"
