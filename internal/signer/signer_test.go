package signer_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/digitorus/pdfsign"
	"github.com/sml/sml-signx/internal/signer"
	"github.com/sml/sml-signx/internal/testutil"
)

func TestSignAndVerify(t *testing.T) {
	dir := t.TempDir()
	pfxPath, password, err := testutil.WritePFX(dir)
	if err != nil {
		t.Fatalf("write pfx: %v", err)
	}

	res, err := signer.Sign(signer.Options{
		PDF:         testutil.MinimalPDF(),
		PFXPath:     pfxPath,
		PFXPassword: password,
		Name:        "Test Signer",
		Designation: "Director",
		Department:  "Finance",
		Reason:      "Approved",
		Location:    "Hyderabad",
		Boxes: []signer.Box{
			{Page: 1, X: 50, Y: 700, Width: 200, Height: 60},
		},
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(res.PDF) == 0 {
		t.Fatal("signed pdf is empty")
	}
	if !bytes.Contains(res.PDF, []byte("/ByteRange")) {
		t.Fatal("signed pdf does not contain a signature ByteRange")
	}
	if res.Certificate == nil || res.Certificate.Subject.CommonName != "SML Test Signer" {
		t.Fatalf("unexpected certificate: %+v", res.Certificate)
	}

	doc, err := pdfsign.Open(bytes.NewReader(res.PDF), int64(len(res.PDF)))
	if err != nil {
		t.Fatalf("open signed pdf: %v", err)
	}
	result := doc.Verify().TrustSelfSigned(true)
	if !result.Valid() {
		t.Fatalf("signature invalid: %v", result.Err())
	}
	if result.Count() != 1 {
		t.Fatalf("signature count = %d, want 1", result.Count())
	}
}

func TestSignWrongPassword(t *testing.T) {
	dir := t.TempDir()
	pfxPath, _, err := testutil.WritePFX(dir)
	if err != nil {
		t.Fatalf("write pfx: %v", err)
	}

	_, err = signer.Sign(signer.Options{
		PDF:         testutil.MinimalPDF(),
		PFXPath:     pfxPath,
		PFXPassword: "not-the-password",
		Boxes:       []signer.Box{{Page: 1, X: 50, Y: 700, Width: 200, Height: 60}},
	})
	if err == nil {
		t.Fatal("expected an error for a wrong PFX password")
	}
}

func TestSignExpiredCertificate(t *testing.T) {
	dir := t.TempDir()
	pfxPath, password, err := testutil.WritePFXWithValidity(dir, time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("write pfx: %v", err)
	}

	_, err = signer.Sign(signer.Options{
		PDF:         testutil.MinimalPDF(),
		PFXPath:     pfxPath,
		PFXPassword: password,
		Boxes:       []signer.Box{{Page: 1, X: 50, Y: 700, Width: 200, Height: 60}},
	})
	if !errors.Is(err, signer.ErrCertificateExpired) {
		t.Fatalf("error = %v, want ErrCertificateExpired", err)
	}
}

func TestSignRequiresBoxes(t *testing.T) {
	_, err := signer.Sign(signer.Options{PDF: testutil.MinimalPDF()})
	if err == nil {
		t.Fatal("expected an error when no signature boxes are provided")
	}
}
