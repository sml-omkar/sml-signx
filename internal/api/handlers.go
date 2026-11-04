package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/sml/sml-signx/internal/signer"
	"github.com/sml/sml-signx/internal/store"
)

func (s *Server) handleRegisterPFX(w http.ResponseWriter, r *http.Request) {
	var req RegisterPFXRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	req.PFXPath = strings.TrimSpace(req.PFXPath)
	req.PFXPassword = strings.TrimSpace(req.PFXPassword)
	if req.PFXPath == "" || req.PFXPassword == "" {
		writeError(w, http.StatusBadRequest, "pfxPath and pfxPassword are required")
		return
	}

	absPath, err := filepath.Abs(req.PFXPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pfxPath: "+err.Error())
		return
	}

	cert, err := signer.Inspect(absPath, req.PFXPassword)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Registration failed: "+err.Error())
		return
	}

	meta := store.MetadataFromCertificate(absPath, strings.TrimSpace(req.PFXLabel), cert)
	if err := s.store.Register(meta); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist certificate: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, RegisterPFXResponse{
		Registered:  true,
		Certificate: meta,
	})
}

func (s *Server) handleListCertificates(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"certificates": s.store.List(),
	})
}

func (s *Server) handleUnregisterPFX(w http.ResponseWriter, r *http.Request) {
	var req UnregisterPFXRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	req.PFXPath = strings.TrimSpace(req.PFXPath)
	if req.PFXPath == "" {
		writeError(w, http.StatusBadRequest, "pfxPath is required")
		return
	}

	absPath, err := filepath.Abs(req.PFXPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pfxPath: "+err.Error())
		return
	}

	removed, err := s.store.Remove(absPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update registry: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, UnregisterPFXResponse{Removed: removed})
}

func (s *Server) handleSign(w http.ResponseWriter, r *http.Request) {
	var req SignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	req.PDFBase64 = strings.TrimSpace(req.PDFBase64)
	req.PFXPath = strings.TrimSpace(req.PFXPath)
	req.PFXPassword = strings.TrimSpace(req.PFXPassword)

	if req.PDFBase64 == "" || req.PFXPath == "" || req.PFXPassword == "" {
		writeError(w, http.StatusBadRequest, "pdfBase64, pfxPath, pfxPassword are required")
		return
	}
	if len(req.Signatures) == 0 {
		writeError(w, http.StatusBadRequest, "signatures list is required")
		return
	}

	pdf, err := decodeBase64PDF(req.PDFBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "pdfBase64 is not valid base64: "+err.Error())
		return
	}

	absPath, err := filepath.Abs(req.PFXPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid pfxPath: "+err.Error())
		return
	}

	if s.cfg.RequireRegisteredPFX {
		if _, ok := s.store.Lookup(absPath); !ok {
			writeError(w, http.StatusForbidden, "PFX_NOT_REGISTERED: register this certificate with POST /op/register-pfx first")
			return
		}
	}

	boxes := make([]signer.Box, 0, len(req.Signatures))
	for _, b := range req.Signatures {
		if b.Page < 1 {
			writeError(w, http.StatusBadRequest, "signature page must be >= 1")
			return
		}
		if b.Width <= 0 || b.Height <= 0 {
			writeError(w, http.StatusBadRequest, "signature width and height must be positive")
			return
		}
		if b.X < 0 || b.Y < 0 {
			writeError(w, http.StatusBadRequest, "signature x and y must be >= 0")
			return
		}
		boxes = append(boxes, signer.Box{
			Page:   b.Page,
			X:      b.X,
			Y:      b.Y,
			Width:  b.Width,
			Height: b.Height,
		})
	}

	tsa := strings.TrimSpace(req.TSAURL)
	if tsa == "" {
		tsa = s.cfg.TSAURL
	}

	result, err := signer.Sign(signer.Options{
		PDF:         pdf,
		PFXPath:     absPath,
		PFXPassword: req.PFXPassword,
		Name:        req.ERPUsername,
		Designation: req.Designation,
		Department:  req.Department,
		Reason:      req.Reason,
		Location:    req.Location,
		ContactInfo: req.ContactInfo,
		Boxes:       boxes,
		TSAURL:      tsa,
	})
	if err != nil {
		switch {
		case errors.Is(err, signer.ErrCertificateExpired):
			writeError(w, http.StatusForbidden, "PFX_EXPIRED: "+err.Error())
		case errors.Is(err, signer.ErrCertificateNotYetValid):
			writeError(w, http.StatusForbidden, "PFX_NOT_YET_VALID: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "Signing failed: "+err.Error())
		}
		return
	}

	org := ""
	if len(result.Certificate.Subject.Organization) > 0 {
		org = result.Certificate.Subject.Organization[0]
	}

	writeJSON(w, http.StatusOK, SignResponse{
		Success:         true,
		SignedPDFBase64: base64.StdEncoding.EncodeToString(result.PDF),
		CertCN:          result.Certificate.Subject.CommonName,
		CertOrg:         org,
		CertSubject:     result.Certificate.Subject.String(),
		SigningTime:     time.Now().UTC(),
		SignatureCount:  len(boxes),
	})
}

func decodeBase64PDF(s string) ([]byte, error) {
	if idx := strings.Index(s, "base64,"); idx >= 0 && strings.HasPrefix(strings.ToLower(s[:idx]), "data:") {
		s = s[idx+len("base64,"):]
	}
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, s)

	if data, err := base64.StdEncoding.DecodeString(s); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
