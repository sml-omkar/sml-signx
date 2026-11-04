#!/usr/bin/env bash
set -euo pipefail
umask 077

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/tmp/real-pfx"
API_KEY="${API_KEY:-test-api-key}"

command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

ARG1="${1:-}"
if [[ "$ARG1" == *.pdf || "$ARG1" == *.PDF ]]; then
  PDF_PATH="$ARG1"
  PFX_PATH="${PFX_PATH:-}"
else
  PFX_PATH="${ARG1:-${PFX_PATH:-}}"
  PDF_PATH="${2:-${PDF_PATH:-}}"
fi
PFX_PATH="${PFX_PATH/#\~/$HOME}"
PDF_PATH="${PDF_PATH/#\~/$HOME}"

if [[ -z "$PFX_PATH" ]]; then
  read -r -p "Path to your .pfx/.p12: " PFX_PATH
  PFX_PATH="${PFX_PATH/#\~/$HOME}"
fi
[[ -f "$PFX_PATH" ]] || { echo "PFX not found: $PFX_PATH" >&2; exit 1; }

read -r -s -p "PFX password: " PFX_PASSWORD
echo
[[ -n "$PFX_PASSWORD" ]] || { echo "Password cannot be empty" >&2; exit 1; }

SIGNER_NAME="${SIGNER_NAME:-SML Test Signer}"
DESIGNATION="${DESIGNATION:-Authorized Signatory}"
REASON="${REASON:-Approved}"
LOCATION="${LOCATION:-Hyderabad}"
SIG_PAGE="${SIG_PAGE:-1}"
SIG_X="${SIG_X:-50}"
SIG_Y="${SIG_Y:-50}"
SIG_W="${SIG_W:-240}"
SIG_H="${SIG_H:-70}"
export SIGNER_NAME DESIGNATION REASON LOCATION SIG_PAGE SIG_X SIG_Y SIG_W SIG_H

mkdir -p "$OUT/data"

echo "==> Building local binary"
(cd "$ROOT" && go build -o bin/sml-signx ./cmd/sml-signx)

if [[ -z "$PDF_PATH" ]]; then
  PDF_PATH="$OUT/invoice.pdf"
  echo "==> No PDF given, creating a sample PDF"
  python3 - "$PDF_PATH" <<'PY'
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
else
  [[ -f "$PDF_PATH" ]] || { echo "PDF not found: $PDF_PATH" >&2; exit 1; }
  python3 - "$PDF_PATH" <<'PY'
import sys
with open(sys.argv[1], "rb") as f:
    if f.read(5) != b"%PDF-":
        sys.exit("Not a PDF file: " + sys.argv[1])
PY
fi

PDF_STEM=$(python3 - "$PDF_PATH" <<'PY'
import os, sys
base = os.path.basename(sys.argv[1])
stem, _ = os.path.splitext(base)
print(stem or base)
PY
)
SIGNED_PATH="$OUT/${PDF_STEM}_signed.pdf"

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
REGISTER_CODE=$(PFX_PASSWORD="$PFX_PASSWORD" python3 - "$PFX_PATH" <<'PY' | \
  curl -sS -o "$OUT/register-response.json" -w '%{http_code}' -X POST "$BASE/op/register-pfx" \
    -H "Content-Type: application/json" \
    -H "X-API-KEY: $API_KEY" \
    --data-binary @-
import json, os, sys
print(json.dumps({
    "pfxPath": sys.argv[1],
    "pfxPassword": os.environ["PFX_PASSWORD"],
    "pfxLabel": "Real certificate test",
}))
PY
)
if [[ "$REGISTER_CODE" != "200" ]]; then
  echo "Registration failed (HTTP $REGISTER_CODE):" >&2
  cat "$OUT/register-response.json" >&2
  echo >&2
  cat "$OUT/server.log" >&2
  exit 1
fi
python3 -m json.tool "$OUT/register-response.json"

echo
echo "==> Signing PDF: $PDF_PATH"
SIGN_CODE=$(PFX_PASSWORD="$PFX_PASSWORD" python3 - "$PDF_PATH" "$PFX_PATH" <<'PY' | \
  curl -sS -o "$OUT/sign-response.json" -w '%{http_code}' -X POST "$BASE/op/sign" \
    -H "Content-Type: application/json" \
    -H "X-API-KEY: $API_KEY" \
    --data-binary @-
import base64, json, os, sys
pdf = base64.b64encode(open(sys.argv[1], "rb").read()).decode()
print(json.dumps({
    "pdfBase64": pdf,
    "pfxPath": sys.argv[2],
    "pfxPassword": os.environ["PFX_PASSWORD"],
    "erpUsername": os.environ["SIGNER_NAME"],
    "designation": os.environ["DESIGNATION"],
    "reason": os.environ["REASON"],
    "location": os.environ["LOCATION"],
    "signatures": [{
        "page": int(os.environ["SIG_PAGE"]),
        "x": float(os.environ["SIG_X"]),
        "y": float(os.environ["SIG_Y"]),
        "width": float(os.environ["SIG_W"]),
        "height": float(os.environ["SIG_H"]),
    }],
}))
PY
)
if [[ "$SIGN_CODE" != "200" ]]; then
  echo "Signing failed (HTTP $SIGN_CODE):" >&2
  cat "$OUT/sign-response.json" >&2
  echo >&2
  cat "$OUT/server.log" >&2
  exit 1
fi

python3 - "$OUT/sign-response.json" "$SIGNED_PATH" <<'PY'
import base64, json, sys
resp = json.load(open(sys.argv[1]))
assert resp.get("success") is True, resp
pdf = base64.b64decode(resp["signedPdfBase64"])
open(sys.argv[2], "wb").write(pdf)
print("certCN:        ", resp["certCN"])
print("certOrg:       ", resp["certOrg"])
print("signatureCount:", resp["signatureCount"])
print("signed bytes:  ", len(pdf))
print("has ByteRange: ", b"/ByteRange" in pdf)
print("has widget:    ", b"/Widget" in pdf)
print("has appearance:", b"/AP" in pdf)
PY
rm -f "$OUT/register-response.json" "$OUT/sign-response.json"

echo
echo "PASS"
echo "Signed PDF: $SIGNED_PATH"
if command -v open >/dev/null 2>&1; then
  echo "Opening it now..."
  open "$SIGNED_PATH"
elif command -v xdg-open >/dev/null 2>&1; then
  echo "Opening it now..."
  xdg-open "$SIGNED_PATH"
fi
