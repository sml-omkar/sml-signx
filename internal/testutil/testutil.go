// Package testutil provides fixtures shared by the test suite.
package testutil

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// MinimalPDF returns a valid single-page A4 PDF.
func MinimalPDF() []byte {
	const content = "BT /F1 12 Tf 72 720 Td (SML SignX test invoice) Tj ET"

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")

	offsets := make([]int, 0, 5)
	obj := func(s string) {
		offsets = append(offsets, b.Len())
		b.WriteString(s)
	}

	obj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	obj("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>\nendobj\n")
	obj(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", len(content), content))
	obj("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	xref := b.Len()
	b.WriteString("xref\n0 6\n")
	b.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)

	return b.Bytes()
}

// WritePFX writes a self-signed PFX valid from one hour ago for 24 hours.
func WritePFX(dir string) (path, password string, err error) {
	return WritePFXWithValidity(dir, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
}

// WritePFXWithValidity writes a self-signed PFX with the given validity window.
func WritePFXWithValidity(dir string, notBefore, notAfter time.Time) (path, password string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName:   "SML Test Signer",
			Organization: []string{"SML"},
			Country:      []string{"IN"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageEmailProtection},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return "", "", err
	}

	password = "test-password"
	pfxData, err := pkcs12.Modern.Encode(key, cert, nil, password)
	if err != nil {
		return "", "", err
	}

	path = filepath.Join(dir, "test-signer.pfx")
	if err := os.WriteFile(path, pfxData, 0o600); err != nil {
		return "", "", err
	}
	return path, password, nil
}
