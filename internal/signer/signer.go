// Package signer loads PFX certificates and applies PAdES digital signatures
// with an optional visible signature box to PDF documents.
package signer

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/digitorus/pdfsign"
	"software.sslmate.com/src/go-pkcs12"
)

var (
	ErrCertificateExpired     = errors.New("certificate is expired")
	ErrCertificateNotYetValid = errors.New("certificate is not yet valid")
)

// Box describes a visible signature rectangle in PDF points.
// The origin (0,0) is the bottom-left corner of the page; A4 is 595x842 pt.
type Box struct {
	Page   int     `json:"page"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Options describes one signing request.
type Options struct {
	PDF         []byte
	PFXPath     string
	PFXPassword string

	Name        string
	Designation string
	Department  string
	Reason      string
	Location    string
	ContactInfo string

	Boxes  []Box
	TSAURL string
}

// Result is the signed document plus signer certificate details.
type Result struct {
	PDF         []byte
	Certificate *x509.Certificate
}

type identity struct {
	signer  crypto.Signer
	cert    *x509.Certificate
	caCerts []*x509.Certificate
}

// Inspect validates a PFX file and returns its leaf certificate.
func Inspect(pfxPath, password string) (*x509.Certificate, error) {
	id, err := loadIdentity(pfxPath, password)
	if err != nil {
		return nil, err
	}
	return id.cert, nil
}

// Sign applies a PAdES digital signature and a visible signature stamp for
// every requested box.
func Sign(opts Options) (*Result, error) {
	if len(opts.PDF) == 0 {
		return nil, fmt.Errorf("pdf is empty")
	}
	if len(opts.Boxes) == 0 {
		return nil, fmt.Errorf("at least one signature box is required")
	}

	id, err := loadIdentity(opts.PFXPath, opts.PFXPassword)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if now.After(id.cert.NotAfter) {
		return nil, fmt.Errorf("%w (expired %s)", ErrCertificateExpired, id.cert.NotAfter.Format(time.RFC3339))
	}
	if now.Before(id.cert.NotBefore) {
		return nil, fmt.Errorf("%w (valid from %s)", ErrCertificateNotYetValid, id.cert.NotBefore.Format(time.RFC3339))
	}

	doc, err := pdfsign.Open(bytes.NewReader(opts.PDF), int64(len(opts.PDF)))
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = id.cert.Subject.CommonName
	}

	for i, box := range opts.Boxes {
		if box.Page < 1 {
			return nil, fmt.Errorf("signature %d: page must be >= 1", i+1)
		}
		if box.Width <= 0 || box.Height <= 0 {
			return nil, fmt.Errorf("signature %d: width and height must be positive", i+1)
		}

		app := buildAppearance(box, opts, name)
		builder := doc.Sign(id.signer, id.cert, id.caCerts...).
			SignerName(name).
			Reason(opts.Reason).
			Location(opts.Location).
			Contact(opts.ContactInfo).
			Type(pdfsign.ApprovalSignature).
			Page(box.Page).
			Appearance(app, box.X, box.Y)

		if tsa := strings.TrimSpace(opts.TSAURL); tsa != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			builder = builder.Format(pdfsign.PAdES_B_T).Timestamp(tsa).Context(ctx)
		} else {
			// PAdES_B is fully offline: no OCSP/CRL or TSA network calls.
			builder = builder.Format(pdfsign.PAdES_B)
		}
	}

	var out bytes.Buffer
	if _, err := doc.Write(&out); err != nil {
		return nil, fmt.Errorf("sign pdf: %w", err)
	}

	return &Result{PDF: out.Bytes(), Certificate: id.cert}, nil
}

func loadIdentity(pfxPath, password string) (*identity, error) {
	data, err := os.ReadFile(pfxPath)
	if err != nil {
		return nil, fmt.Errorf("read pfx: %w", err)
	}

	key, cert, caCerts, err := pkcs12.DecodeChain(data, password)
	if err != nil {
		return nil, fmt.Errorf("decode pfx (wrong password or corrupt file): %w", err)
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("pfx private key does not support signing")
	}

	return &identity{signer: signer, cert: cert, caCerts: caCerts}, nil
}

// buildAppearance lays out a compact signature stamp. Font sizes scale with
// the requested box height so small boxes remain readable.
func buildAppearance(box Box, opts Options, name string) *pdfsign.Appearance {
	app := pdfsign.NewAppearance(box.Width, box.Height)
	app.Border(0.75, 120, 120, 120)

	fontSize := math.Min(9, math.Max(5, box.Height/9))
	lineHeight := fontSize * 1.35
	padding := math.Max(3, fontSize*0.5)

	lines := []appearanceLine{
		{text: "Digitally signed by {{Name}}", bold: true, color: [3]uint8{0, 0, 0}},
	}

	role := strings.TrimSpace(strings.Join(nonEmpty(opts.Designation, opts.Department), ", "))
	if role != "" {
		lines = append(lines, appearanceLine{text: role, color: [3]uint8{40, 40, 40}})
	}
	lines = append(lines,
		appearanceLine{text: "Reason: {{Reason}}", color: [3]uint8{60, 60, 60}},
		appearanceLine{text: "Location: {{Location}}", color: [3]uint8{60, 60, 60}},
		appearanceLine{text: "Date: {{Date}}", color: [3]uint8{60, 60, 60}},
	)

	y := box.Height - padding - fontSize
	for _, line := range lines {
		if y < padding {
			break
		}
		font := pdfsign.StandardFont(pdfsign.Helvetica)
		size := fontSize - 0.5
		if line.bold {
			font = pdfsign.StandardFont(pdfsign.HelveticaBold)
			size = fontSize
		}
		app.Text(line.text).
			Font(font, size).
			SetColor(line.color[0], line.color[1], line.color[2]).
			Position(padding, y)
		y -= lineHeight
	}

	return app
}

type appearanceLine struct {
	text  string
	bold  bool
	color [3]uint8
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
