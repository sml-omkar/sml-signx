# SML SignX

Local PDF signing service for Windows and Linux. It signs PDFs with a PFX
certificate and exposes a small REST API so SAP, Tally, Odoo or any custom
application can request signatures. Documents and private keys never leave the
machine.

This is an internal tool: there is no licensing, activation, quota or master
server. The service only does three things:

1. Register a PFX certificate by file path.
2. Sign a PDF with PAdES + a visible signature stamp.
3. Report status.

## How it works

```
SAP / ERP ──HTTP──> sml-signx (127.0.0.1:7070) ──> PFX certificate on disk
                          │
                          └──> signed PDF (Base64) back to the caller
```

- Pure Go, single self-contained binary per OS.
- `CGO_ENABLED=0`, so cross-compilation and clean installers are trivial.
- Windows build runs in the system tray; Linux build runs headless under systemd.
- PAdES baseline signatures are produced offline (no OCSP/CRL calls). An
  optional RFC 3161 timestamp authority can be enabled.

## Quick start

```bash
# Build for the current OS
make build

# Run
./bin/sml-signx --port 7070
```

The service listens on `http://127.0.0.1:7070` by default. Bind to a different
interface only if SAP runs on another machine:

```bash
./bin/sml-signx --host 0.0.0.0 --port 7070 --api-key "long-random-secret"
```

## API

All request and response bodies are JSON. When `--api-key` is set, send the key
in `X-API-KEY` or `Authorization: Bearer <key>`.

| Method | Path                   | Purpose                              |
|--------|------------------------|--------------------------------------|
| GET    | `/healthz`             | Liveness probe (always public)        |
| GET    | `/`                    | Service info and endpoint list        |
| GET    | `/op/status`           | Status, configuration, certificates   |
| GET    | `/op/certificates`     | List registered PFX certificates      |
| POST   | `/op/register-pfx`     | Register a PFX by file path           |
| POST   | `/op/unregister-pfx`   | Remove a registered PFX               |
| POST   | `/op/sign`             | Sign a PDF (Base64 in, Base64 out)    |

### Register a certificate

Passwords are used to validate the PFX and are **not** persisted.

```bash
curl -X POST http://127.0.0.1:7070/op/register-pfx \
  -H "Content-Type: application/json" \
  -H "X-API-KEY: long-random-secret" \
  -d '{
    "pfxPath": "C:\\Certs\\company.pfx",
    "pfxPassword": "YourPassword",
    "pfxLabel": "Company Certificate"
  }'
```

Response:

```json
{
  "registered": true,
  "certificate": {
    "path": "C:\\Certs\\company.pfx",
    "label": "Company Certificate",
    "subject": "CN=Company, O=ACME, C=IN",
    "commonName": "Company",
    "organization": "ACME",
    "serialNumber": "123456789",
    "notBefore": "2026-01-01T00:00:00Z",
    "notAfter": "2028-01-01T00:00:00Z",
    "sha256": "AB12...",
    "registeredAt": "2026-11-04T12:00:00Z"
  }
}
```

### Sign a PDF

```bash
PDF_B64=$(base64 -w 0 invoice.pdf)   # macOS: base64 -i invoice.pdf

curl -s -X POST http://127.0.0.1:7070/op/sign \
  -H "Content-Type: application/json" \
  -H "X-API-KEY: long-random-secret" \
  -d "{
    \"pdfBase64\": \"$PDF_B64\",
    \"pfxPath\": \"/opt/certs/company.pfx\",
    \"pfxPassword\": \"YourPassword\",
    \"erpUsername\": \"A. Signer\",
    \"designation\": \"Director\",
    \"department\": \"Finance\",
    \"reason\": \"Approved\",
    \"location\": \"Hyderabad\",
    \"signatures\": [
      { \"page\": 1, \"x\": 50, \"y\": 700, \"width\": 200, \"height\": 60 }
    ]
  }" > response.json
```

Decode the signed PDF:

```bash
python3 -c "import json,base64; \
d=json.load(open('response.json')); \
open('invoice_signed.pdf','wb').write(base64.b64decode(d['signedPdfBase64']))"
```

Response fields:

```json
{
  "success": true,
  "signedPdfBase64": "JVBERi0xLjQK...",
  "certCN": "Company",
  "certOrg": "ACME",
  "certSubject": "CN=Company, O=ACME, C=IN",
  "signingTime": "2026-11-04T12:00:00Z",
  "signatureCount": 1
}
```

`pdfBase64` also accepts a `data:application/pdf;base64,...` prefix.

### Check status

```bash
curl http://127.0.0.1:7070/op/status -H "X-API-KEY: long-random-secret"
```

### Remove a certificate

```bash
curl -X POST http://127.0.0.1:7070/op/unregister-pfx \
  -H "Content-Type: application/json" \
  -H "X-API-KEY: long-random-secret" \
  -d '{"pfxPath": "/opt/certs/company.pfx"}'
```

### Coordinate system

Signature boxes use PDF points (1 pt = 1/72 inch), origin at the **bottom-left**
of the page. A4 is 595 x 842 pt.

| Position     | x   | y   | width | height |
|--------------|-----|-----|-------|--------|
| Bottom-left  | 50  | 50  | 200   | 60     |
| Bottom-right | 345 | 50  | 200   | 60     |
| Top-left     | 50  | 750 | 200   | 60     |
| Top-right    | 345 | 750 | 200   | 60     |

## Configuration

Environment variables are read first; command-line flags override them.

| Flag                      | Environment                    | Default                         |
|---------------------------|--------------------------------|---------------------------------|
| `--host`                  | `SML_SIGNING_HOST`             | `127.0.0.1`                     |
| `--port`                  | `SML_SIGNING_PORT`             | `7070`                          |
| `--data-dir`              | `SML_DATA_DIR`                 | OS user config dir / `SMLSignX` |
| `--api-key`               | `SML_API_KEY`                  | empty (auth disabled)           |
| `--allow-unregistered-pfx`| `SML_ALLOW_UNREGISTERED_PFX`   | `false`                         |
| `--tsa`                   | `SML_TSA_URL`                  | empty (PAdES B-B, offline)      |
| `--headless`              | n/a                            | `false`                         |

`--allow-unregistered-pfx` lets callers sign with any readable PFX path. By
default signing is blocked until `POST /op/register-pfx` succeeds.

### Data and logs

| OS      | Data directory                     | Log file                          |
|---------|------------------------------------|-----------------------------------|
| Windows | `%APPDATA%\SMLSignX\`              | `%APPDATA%\SMLSignX\sml-signx.log`|
| Linux   | `~/.config/SMLSignX/` (or `SML_DATA_DIR`) | `sml-signx.log`            |

The registry lives in `certs.json` (mode `0600`) and contains certificate
metadata only. PFX passwords are never written to disk.

## Build

```bash
make test        # unit + end-to-end API tests
make vet
make linux       # dist/sml-signx-linux-amd64
make windows     # dist/sml-signx-windows-amd64.exe (GUI subsystem, tray)
make darwin      # dist/sml-signx-darwin-arm64 (dev/testing)
```

Version metadata is injected at build time:

```bash
make windows VERSION=1.0.0
```

### Local testing (macOS/Linux)

```bash
make smoke
```

Generates a self-signed PFX and sample PDF, starts the service on a free
loopback port, registers the certificate, signs the PDF, and verifies the
result. Output is written to `tmp/smoke/invoice_signed.pdf`.

To test your real certificate:

```bash
./scripts/test-real-pfx.sh /path/to/certificate.pfx
```

To sign your own PDF instead of the generated sample:

```bash
./scripts/test-real-pfx.sh /path/to/certificate.pfx "/path/to/Your Invoice.pdf"
# or let the script prompt for the certificate:
./scripts/test-real-pfx.sh "/path/to/Your Invoice.pdf"
```

The signed file is written to `tmp/real-pfx/<name>_signed.pdf` and opened.
Adjust the stamp position with `SIG_PAGE`, `SIG_X`, `SIG_Y`, `SIG_W`, `SIG_H`
(PDF points, origin bottom-left):

```bash
SIG_X=340 SIG_Y=740 SIG_W=220 SIG_H=70 \
  ./scripts/test-real-pfx.sh cert.pfx "Your Invoice.pdf"
```

The script prompts for the PFX password without echoing it, never writes it to
disk, and sends it only to the locally running service.

## Install

### Windows

1. Build `dist/sml-signx-windows-amd64.exe` with `make windows`.
2. Install [Inno Setup](https://jrsoftware.org/isinfo.php).
3. Compile `installers/windows/sml-signx.iss` (Inno Setup Compiler).
4. Run `dist/SML-SignX-Setup-<version>.exe`.

The installer copies the binary to `Program Files\SML SignX`, adds the tray app
to the current user's login `Run` key, and creates Start Menu shortcuts.

The tray menu opens the status page and data folder, and quits the service.
The service can also be run with `--headless` for command-line/server use.

### Linux (systemd)

```bash
make linux
sudo ./installers/linux/install.sh dist/sml-signx-linux-amd64
```

This creates a `sml-signx` system user, installs the binary to
`/opt/sml-signx`, uses `/var/lib/sml-signx` as the data directory, installs
`/etc/systemd/system/sml-signx.service` and starts the service.

```bash
systemctl status sml-signx
journalctl -u sml-signx -f
```

Edit `/etc/systemd/system/sml-signx.service` to set `SML_API_KEY` or
`SML_TSA_URL`, then `systemctl daemon-reload && systemctl restart sml-signx`.

## Security notes

- The default bind address is loopback (`127.0.0.1`). Set an API key before
  binding to `0.0.0.0` or a LAN interface.
- PFX passwords are supplied per request and never stored.
- Signing is offline by default (PAdES B-B). Enabling a TSA adds an outbound
  HTTPS request per signature.
- Run the Linux service as the dedicated `sml-signx` user; keep PFX files
  readable only by that user.
- Certificate expiry and future-dated certificates are rejected with
  `403 PFX_EXPIRED` / `403 PFX_NOT_YET_VALID`.

## Error format

Errors use the same shape as the reference utility:

```json
{ "error": "PFX_NOT_REGISTERED: register this certificate with POST /op/register-pfx first" }
```

| HTTP | Meaning                                                        |
|------|----------------------------------------------------------------|
| 400  | Missing/invalid fields, bad Base64, invalid signature box      |
| 401  | Missing or wrong API key                                       |
| 403  | PFX not registered, expired, not yet valid                     |
| 413  | Request body larger than 128 MiB                               |
| 500  | PDF/PFX processing failure                                     |

## Relationship to SuTan OP Utility

The API shape intentionally mirrors the familiar workflow (`register-pfx` then
`sign`), but this is an internal tool:

- No license activation, quota, partner API key or master-server sync.
- No PFX slot limits or organisation whitelist.
- Offline PAdES B-B by default; SuTan's legacy profile fetches revocation data.
- Pure Go, zero CGO, single binary, cross-compiled from any OS.

## Troubleshooting

- **`PFX_NOT_REGISTERED`** — call `/op/register-pfx` with the same absolute
  path you pass to `/op/sign`.
- **`Registration failed: decode pfx ...`** — wrong password or unsupported PFX
  encoding.
- **`Signing failed: open pdf ...`** — the Base64 payload is not a valid PDF, or
  the PDF is encrypted.
- **Cannot reach the service from SAP** — if SAP is on another host, bind
  `SML_SIGNING_HOST=0.0.0.0`, set `SML_API_KEY`, and open the port in the
  firewall.
