package api

import (
	"time"

	"github.com/sml/sml-signx/internal/store"
)

type RegisterPFXRequest struct {
	PFXPath     string `json:"pfxPath"`
	PFXPassword string `json:"pfxPassword"`
	PFXLabel    string `json:"pfxLabel,omitempty"`
}

type RegisterPFXResponse struct {
	Registered  bool              `json:"registered"`
	Certificate store.Certificate `json:"certificate"`
}

type UnregisterPFXRequest struct {
	PFXPath string `json:"pfxPath"`
}

type UnregisterPFXResponse struct {
	Removed bool `json:"removed"`
}

type SignatureBox struct {
	Page   int     `json:"page"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type SignRequest struct {
	PDFBase64   string `json:"pdfBase64"`
	PFXPath     string `json:"pfxPath"`
	PFXPassword string `json:"pfxPassword"`

	ERPUsername string `json:"erpUsername,omitempty"`
	Designation string `json:"designation,omitempty"`
	Department  string `json:"department,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Location    string `json:"location,omitempty"`
	ContactInfo string `json:"contactInfo,omitempty"`

	Signatures []SignatureBox `json:"signatures"`
	TSAURL     string         `json:"tsaUrl,omitempty"`
}

type SignResponse struct {
	Success         bool      `json:"success"`
	SignedPDFBase64 string    `json:"signedPdfBase64"`
	CertCN          string    `json:"certCN"`
	CertOrg         string    `json:"certOrg"`
	CertSubject     string    `json:"certSubject"`
	SigningTime     time.Time `json:"signingTime"`
	SignatureCount  int       `json:"signatureCount"`
}

type StatusResponse struct {
	Service              string              `json:"service"`
	Version              string              `json:"version"`
	Hostname             string              `json:"hostname"`
	Platform             string              `json:"platform"`
	Address              string              `json:"address"`
	UptimeSeconds        int64               `json:"uptimeSeconds"`
	APIKeyRequired       bool                `json:"apiKeyRequired"`
	RequireRegisteredPFX bool                `json:"requireRegisteredPFX"`
	TSAEnabled           bool                `json:"tsaEnabled"`
	CertificateCount     int                 `json:"certificateCount"`
	Certificates         []store.Certificate `json:"certificates"`
}

type ServiceInfo struct {
	Service   string         `json:"service"`
	Version   string         `json:"version"`
	Status    string         `json:"status"`
	Endpoints []EndpointInfo `json:"endpoints"`
}

type EndpointInfo struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
