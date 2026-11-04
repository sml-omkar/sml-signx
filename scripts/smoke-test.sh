#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/tmp/smoke"
API_KEY="${API_KEY:-test-api-key}"
PFX_PASSWORD="${PFX_PASSWORD:-smokepass}"

command -v openssl >/dev/null || { echo "openssl is required" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

PORT="${PORT:-$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)}"
BASE="http://127.0.0.1:${PORT}"

rm -rf "$OUT"
mkdir -p "$OUT/data"

echo "==> Building local binary"
(cd "$ROOT" && go build -o bin/sml-signx ./cmd/sml-signx)

echo "==> Creating test PFX certificate"
openssl req -x509 -newkey rsa:2048 \
  -keyout "$OUT/key.pem" -out "$OUT/cert.pem" \
  -days 365 -nodes -subj "/CN=SML Local Test/O=SML/C=IN" \
  -addext "basicConstraints=critical,CA:FALSE" \
  -addext "keyUsage=critical,digitalSignature,nonRepudiation" \
  -addext "extendedKeyUsage=emailProtection" >/dev/null 2>&1
openssl pkcs12 -export \
  -out "$OUT/test.pfx" -inkey "$OUT/key.pem" -in "$OUT/cert.pem" \
  -passout "pass:${PFX_PASSWORD}" >/dev/null 2>&1

echo "==> Creating sample PDF"
python3 - "$OUT/invoice.pdf" <<'PY'
import sys
content = b"BT /F1 12 Tf 72 720 Td (SML SignX local test invoice) Tj ET"
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

echo "==> Starting sml-signx on port ${PORT}"
"$ROOT/bin/sml-signx" \
  --host 127.0.0.1 --port "$PORT" \
  --data-dir "$OUT/data" --api-key "$API_KEY" \
  >"$OUT/server.log" 2>&1 &
PID=$!
cleanup() {
  kill "$PID" 2>/dev/null || true
  wait "$PID" 2>/dev/null || true
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
echo "==> /healthz"
curl -fsS "$BASE/healthz"; echo

echo
echo "==> auth check (expect 401 without key)"
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/op/status")
echo "HTTP $CODE"
[[ "$CODE" == "401" ]]

echo
echo "==> register-pfx"
python3 - "$OUT" <<'PY'
import json, sys
out = sys.argv[1]
json.dump({
    "pfxPath": out + "/test.pfx",
    "pfxPassword": "smokepass",
    "pfxLabel": "Local Test Certificate",
}, open(out + "/register.json", "w"))
PY
curl -fsS -X POST "$BASE/op/register-pfx" \
  -H "Content-Type: application/json" \
  -H "X-API-KEY: $API_KEY" \
  -d @"$OUT/register.json" | python3 -m json.tool

echo
echo "==> /op/status"
curl -fsS "$BASE/op/status" -H "X-API-KEY: $API_KEY" | python3 -m json.tool

echo
echo "==> /op/sign"
python3 - "$OUT" <<'PY'
import base64, json, sys
out = sys.argv[1]
pdf = base64.b64encode(open(out + "/invoice.pdf", "rb").read()).decode()
json.dump({
    "pdfBase64": pdf,
    "pfxPath": out + "/test.pfx",
    "pfxPassword": "smokepass",
    "erpUsername": "SML Local Test",
    "designation": "Director",
    "department": "Finance",
    "reason": "Approved",
    "location": "Hyderabad",
    "signatures": [{"page": 1, "x": 50, "y": 700, "width": 220, "height": 70}],
}, open(out + "/sign.json", "w"))
PY
curl -fsS -X POST "$BASE/op/sign" \
  -H "Content-Type: application/json" \
  -H "X-API-KEY: $API_KEY" \
  -d @"$OUT/sign.json" >"$OUT/response.json"

python3 - "$OUT" <<'PY'
import base64, json, sys
out = sys.argv[1]
resp = json.load(open(out + "/response.json"))
assert resp.get("success") is True, resp
pdf = base64.b64decode(resp["signedPdfBase64"])
open(out + "/invoice_signed.pdf", "wb").write(pdf)
print("certCN:        ", resp["certCN"])
print("certOrg:       ", resp["certOrg"])
print("signatureCount:", resp["signatureCount"])
print("signed bytes:  ", len(pdf))
print("has ByteRange: ", b"/ByteRange" in pdf)
print("has widget:    ", b"/Widget" in pdf)
print("has appearance:", b"/AP" in pdf)
PY

echo
echo "==> Server log"
cat "$OUT/server.log"

echo
echo "PASS"
echo "Signed PDF: $OUT/invoice_signed.pdf"
echo "Open it with: open \"$OUT/invoice_signed.pdf\""
echo
echo "To keep testing manually:"
echo "  ./bin/sml-signx --host 127.0.0.1 --port 7070 --data-dir \"$OUT/data\" --api-key \"$API_KEY\""
