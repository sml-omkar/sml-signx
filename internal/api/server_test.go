package api_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/digitorus/pdfsign"
	"github.com/sml/sml-signx/internal/api"
	"github.com/sml/sml-signx/internal/config"
	"github.com/sml/sml-signx/internal/store"
	"github.com/sml/sml-signx/internal/testutil"
)

const testAPIKey = "test-api-key"

type testEnv struct {
	server   *httptest.Server
	pfxPath  string
	password string
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()

	dir := t.TempDir()
	pfxPath, password, err := testutil.WritePFX(dir)
	if err != nil {
		t.Fatalf("write pfx: %v", err)
	}

	cfg := &config.Config{
		Host:                 "127.0.0.1",
		Port:                 7070,
		DataDir:              filepath.Join(dir, "data"),
		APIKey:               testAPIKey,
		RequireRegisteredPFX: true,
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(api.New(cfg, st, logger).Handler())
	t.Cleanup(ts.Close)

	return testEnv{server: ts, pfxPath: pfxPath, password: password}
}

func (e testEnv) do(t *testing.T, method, path, apiKey string, body any) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("X-API-KEY", apiKey)
	}

	resp, err := e.server.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, data
}

func TestHealthIsPublic(t *testing.T) {
	env := newTestEnv(t)
	status, body := env.do(t, http.MethodGet, "/healthz", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestAPIKeyProtectsOPRoutes(t *testing.T) {
	env := newTestEnv(t)

	status, _ := env.do(t, http.MethodGet, "/op/status", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("without key status = %d, want 401", status)
	}

	status, body := env.do(t, http.MethodGet, "/op/status", testAPIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("with key status = %d, body = %s", status, body)
	}
}

func TestRegisterAndSignFlow(t *testing.T) {
	env := newTestEnv(t)

	// Register without a key is rejected.
	registerBody := map[string]any{
		"pfxPath":     env.pfxPath,
		"pfxPassword": env.password,
		"pfxLabel":    "Test Certificate",
	}
	status, _ := env.do(t, http.MethodPost, "/op/register-pfx", "", registerBody)
	if status != http.StatusUnauthorized {
		t.Fatalf("register without key status = %d, want 401", status)
	}

	// Register with a key succeeds.
	status, body := env.do(t, http.MethodPost, "/op/register-pfx", testAPIKey, registerBody)
	if status != http.StatusOK {
		t.Fatalf("register status = %d, body = %s", status, body)
	}
	var reg api.RegisterPFXResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if !reg.Registered || reg.Certificate.CommonName != "SML Test Signer" {
		t.Fatalf("unexpected register response: %+v", reg)
	}

	// Status reports the certificate.
	status, body = env.do(t, http.MethodGet, "/op/status", testAPIKey, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	var st api.StatusResponse
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if st.CertificateCount != 1 {
		t.Fatalf("certificate count = %d, want 1", st.CertificateCount)
	}

	// Sign a PDF.
	signBody := map[string]any{
		"pdfBase64":   base64.StdEncoding.EncodeToString(testutil.MinimalPDF()),
		"pfxPath":     env.pfxPath,
		"pfxPassword": env.password,
		"erpUsername": "Test Signer",
		"designation": "Director",
		"department":  "Finance",
		"reason":      "Approved",
		"location":    "Hyderabad",
		"signatures": []map[string]any{
			{"page": 1, "x": 50, "y": 700, "width": 200, "height": 60},
		},
	}
	status, body = env.do(t, http.MethodPost, "/op/sign", testAPIKey, signBody)
	if status != http.StatusOK {
		t.Fatalf("sign status = %d, body = %s", status, body)
	}

	var signResp api.SignResponse
	if err := json.Unmarshal(body, &signResp); err != nil {
		t.Fatalf("decode sign response: %v", err)
	}
	if !signResp.Success || signResp.SignedPDFBase64 == "" {
		t.Fatalf("unexpected sign response: %+v", signResp)
	}

	signed, err := base64.StdEncoding.DecodeString(signResp.SignedPDFBase64)
	if err != nil {
		t.Fatalf("decode signed pdf: %v", err)
	}
	doc, err := pdfsign.Open(bytes.NewReader(signed), int64(len(signed)))
	if err != nil {
		t.Fatalf("open signed pdf: %v", err)
	}
	if result := doc.Verify().TrustSelfSigned(true); !result.Valid() {
		t.Fatalf("signed pdf invalid: %v", result.Err())
	}
}

func TestSignRejectsUnregisteredPFX(t *testing.T) {
	env := newTestEnv(t)

	signBody := map[string]any{
		"pdfBase64":   base64.StdEncoding.EncodeToString(testutil.MinimalPDF()),
		"pfxPath":     filepath.Join(t.TempDir(), "not-registered.pfx"),
		"pfxPassword": "whatever",
		"signatures": []map[string]any{
			{"page": 1, "x": 50, "y": 700, "width": 200, "height": 60},
		},
	}
	status, body := env.do(t, http.MethodPost, "/op/sign", testAPIKey, signBody)
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s; want 403", status, body)
	}
}

func TestSignValidation(t *testing.T) {
	env := newTestEnv(t)

	status, _ := env.do(t, http.MethodPost, "/op/sign", testAPIKey, map[string]any{})
	if status != http.StatusBadRequest {
		t.Fatalf("empty body status = %d, want 400", status)
	}

	status, _ = env.do(t, http.MethodPost, "/op/sign", testAPIKey, map[string]any{
		"pdfBase64":   base64.StdEncoding.EncodeToString(testutil.MinimalPDF()),
		"pfxPath":     env.pfxPath,
		"pfxPassword": env.password,
		"signatures":  []map[string]any{},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("empty signatures status = %d, want 400", status)
	}
}
