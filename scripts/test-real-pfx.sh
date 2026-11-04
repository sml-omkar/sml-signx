#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/tmp/real-pfx"
API_KEY="${API_KEY:-test-api-key}"

command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

PFX_PATH="${1:-${PFX_PATH:-}}"
if [[ -z "$PFX_PATH" ]]; then
  read -r -p "Path to your .pfx/.p12: " PFX_PATH
fi
PFX_PATH="${PFX_PATH/#\~/$HOME}"
[[ -f "$PFX_PATH" ]] || { echo "File not found: $PFX_PATH" >&2; exit 1; }

read -r -s -p "PFX password: " PFX_PASSWORD
echo
[[ -n "$PFX_PASSWORD" ]] || { echo "Password cannot be empty" >&2; exit 1; }

SIGNER_NAME="${SIGNER_NAME:-SML Test Signer}"
DESIGNATION="${DESIGNATION:-Authorized Signatory}"
REASON="${REASON:-Approved}"
LOCATION="${LOCATION:-Hyderabad}"
export PFX_PATH SIGNER_NAME DESIGNATION REASON LOCATION

mkdir -p "$OUT/data"

echo "==> Building local binary"
(cd "$ROOT" && go build -o bin/sml-signx ./cmd/sml-signx)

if [[ ! -f "$OUT/invoice.pdf" ]]; then
  python3 - "$OUT/invoice.pdf" <<'PY'
import sys
content = b"BT /F1 12 Tf 72 720 Td (SML SignX real certificate test) Tj ET"
objs = [
    b"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
    b"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
    b"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>\nendobj\n",
    b"4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n" % (len(content), content),
    b"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
]
out = bytearray(b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
offsets = []
for obj in objs:
    offsets.append(len(out))
    out += obj
xref = len(out)
out += b"xref\n0 %d\n0000000000 65535 f \n" % (len(objs) + 1)
for off in offsets:
    out += ("%010d 00000 n \n" % off).encode()
out += ("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n" % (len(objs) + 1, xref)).encode()
open(sys.argv[1], "wb").write(out)
PY
fi

PORT="${PORT:-$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)}"
BASE="http://127.0.0.1:${PORT}"

echo "==> Starting sml-signx on port ${PORT}"
"$ROOT/bin/sml-signx" \
  --host 127.0.0.1 --port "$PORT" \
  --data-dir "$OUT/data" --api-key "$API_KEY" \
  >"$OUT/server.log" 2>&1 &
PID=$!
cleanup() {
  kill "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
  unset PFX_PASSWORD
}
trap cleanup EXIT

for _ in $(seq 1 50); do
  if curl -fsS "$BASE/healthz" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "server failed to start" >&2
    cat "$OUT/server.log" >&2
    exit 1
  fi
  sleep 0.2
done

echo
echo "==> Registering certificate"
REGISTER_BODY=$(PFX_PASSWORD="$PFX_PASSWORD" python3 - <<'PY'
import json, os
print(json.dumps({
    "pfxPath": os.environ["PFX_PATH"],
    "pfxPassword": os.environ["PFX_PASSWORD"],
    "pfxLabel": "Real certificate test",
}))
PY
)
REGISTER_RESP=$(printf '%s' "$REGISTER_BODY" | curl -sS -w "\n%{http_code}" -X POST "$BASE/op/register-pfx" \
  -H "Content-Type: application/json" \
  -H "X-API-KEY: $API_KEY" \
  --data-binary @-)
unset REGISTER_BODY
REGISTER_CODE="${REGISTER_RESP##*$'\n'}"
REGISTER_DATA="${REGISTER_RESP%$'\n'*}"
if [[ "$REGISTER_CODE" != "200" ]]; then
  echo "Registration failed (HTTP $REGISTER_CODE):" >&2
  echo "$REGISTER_DATA" >&2
  echo >&2
  cat "$OUT/server.log" >&2
  exit 1
fi
printf '%s' "$REGISTER_DATA" | python3 -m json.tool

echo
echo "==> Signing sample PDF"
SIGN_BODY=$(PFX_PASSWORD="$PFX_PASSWORD" python3 - "$OUT" <<'PY'
import base64, json, os, sys
pdf = base64.b64encode(open(os.path.join(sys.argv[1], "invoice.pdf"), "rb").read()).decode()
print(json.dumps({
    "pdfBase64": pdf,
    "pfxPath": os.environ["PFX_PATH"],
    "pfxPassword": os.environ["PFX_PASSWORD"],
    "erpUsername": os.environ["SIGNER_NAME"],
    "designation": os.environ["DESIGNATION"],
    "reason": os.environ["REASON"],
    "location": os.environ["LOCATION"],
    "signatures": [{"page": 1, "x": 50, "y": 700, "width": 240, "height": 70}],
}))
PY
)
SIGN_RESP=$(printf '%s' "$SIGN_BODY" | curl -sS -w "\n%{http_code}" -X POST "$BASE/op/sign" \
  -H "Content-Type: application/json" \
  -H "X-API-KEY: $API_KEY" \
  --data-binary @-)
unset SIGN_BODY
SIGN_CODE="${SIGN_RESP##*$'\n'}"
SIGN_DATA="${SIGN_RESP%$'\n'*}"
if [[ "$SIGN_CODE" != "200" ]]; then
  echo "Signing failed (HTTP $SIGN_CODE):" >&2
  echo "$SIGN_DATA" >&2
  echo >&2
  cat "$OUT/server.log" >&2
  exit 1
fi

printf '%s' "$SIGN_DATA" | python3 -c '
import base64, json, sys
resp = json.load(sys.stdin)
assert resp.get("success") is True, resp
pdf = base64.b64decode(resp["signedPdfBase64"])
open(sys.argv[1] + "/invoice_signed.pdf", "wb").write(pdf)
print("certCN:        ", resp["certCN"])
print("certOrg:       ", resp["certOrg"])
print("signatureCount:", resp["signatureCount"])
print("signed bytes:  ", len(pdf))
print("has ByteRange: ", b"/ByteRange" in pdf)
print("has widget:    ", b"/Widget" in pdf)
print("has appearance:", b"/AP" in pdf)
' "$OUT"
unset SIGN_DATA

echo
echo "PASS"
echo "Signed PDF: $OUT/invoice_signed.pdf"
if command -v open >/dev/null 2>&1; then
  echo "Opening it now..."
  open "$OUT/invoice_signed.pdf"
elif command -v xdg-open >/dev/null 2>&1; then
  echo "Opening it now..."
  xdg-open "$OUT/invoice_signed.pdf"
fi
