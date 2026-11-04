package store

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Certificate is the non-secret metadata for a registered PFX file.
// Passwords are never persisted.
type Certificate struct {
	Path         string    `json:"path"`
	Label        string    `json:"label,omitempty"`
	Subject      string    `json:"subject"`
	CommonName   string    `json:"commonName"`
	Organization string    `json:"organization,omitempty"`
	SerialNumber string    `json:"serialNumber"`
	NotBefore    time.Time `json:"notBefore"`
	NotAfter     time.Time `json:"notAfter"`
	SHA256       string    `json:"sha256"`
	RegisteredAt time.Time `json:"registeredAt"`
}

func (c Certificate) Expired(now time.Time) bool {
	return now.After(c.NotAfter)
}

type fileFormat struct {
	Certificates []Certificate `json:"certificates"`
}

// Store persists the PFX registry as a JSON file in the application data dir.
type Store struct {
	mu    sync.RWMutex
	path  string
	certs []Certificate
}

// Open loads (or creates) the registry at dataDir/certs.json.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &Store{path: filepath.Join(dataDir, "certs.json")}

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	if len(data) == 0 {
		return s, nil
	}

	var ff fileFormat
	if err := json.Unmarshal(data, &ff); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", s.path, err)
	}
	s.certs = ff.Certificates
	return s, nil
}

func (s *Store) Path() string { return s.path }

// List returns a copy of all registered certificates.
func (s *Store) List() []Certificate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Certificate, len(s.certs))
	copy(out, s.certs)
	return out
}

// Lookup finds a certificate by PFX path.
func (s *Store) Lookup(path string) (Certificate, bool) {
	key := pathKey(path)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.certs {
		if pathKey(c.Path) == key {
			return c, true
		}
	}
	return Certificate{}, false
}

// Register inserts or updates a certificate entry keyed by PFX path.
func (s *Store) Register(meta Certificate) error {
	key := pathKey(meta.Path)
	if meta.RegisteredAt.IsZero() {
		meta.RegisteredAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	replaced := false
	for i, c := range s.certs {
		if pathKey(c.Path) == key {
			if meta.RegisteredAt.IsZero() {
				meta.RegisteredAt = c.RegisteredAt
			}
			s.certs[i] = meta
			replaced = true
			break
		}
	}
	if !replaced {
		s.certs = append(s.certs, meta)
	}
	return s.saveLocked()
}

// Remove deletes a certificate entry by PFX path. It reports whether an entry
// was removed.
func (s *Store) Remove(path string) (bool, error) {
	key := pathKey(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	filtered := s.certs[:0]
	removed := false
	for _, c := range s.certs {
		if pathKey(c.Path) == key {
			removed = true
			continue
		}
		filtered = append(filtered, c)
	}
	s.certs = filtered
	if !removed {
		return false, nil
	}
	return true, s.saveLocked()
}

func (s *Store) saveLocked() error {
	ff := fileFormat{Certificates: s.certs}
	if ff.Certificates == nil {
		ff.Certificates = []Certificate{}
	}
	data, err := json.MarshalIndent(ff, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.path), "certs-*.json")
	if err != nil {
		return fmt.Errorf("create temp registry: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp registry: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp registry: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp registry: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp registry: %w", err)
	}

	if err := os.Rename(tmpName, s.path); err != nil {
		// Windows cannot rename over an existing file.
		if runtime.GOOS == "windows" {
			if removeErr := os.Remove(s.path); removeErr == nil {
				if retryErr := os.Rename(tmpName, s.path); retryErr == nil {
					return nil
				}
			}
		}
		return fmt.Errorf("replace registry: %w", err)
	}
	return nil
}

// pathKey normalises a filesystem path for comparison. Windows paths are
// compared case-insensitively.
func pathKey(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// MetadataFromCertificate builds registry metadata from a parsed certificate.
func MetadataFromCertificate(path, label string, cert *x509.Certificate) Certificate {
	sum := sha256.Sum256(cert.Raw)
	org := ""
	if len(cert.Subject.Organization) > 0 {
		org = cert.Subject.Organization[0]
	}
	return Certificate{
		Path:         path,
		Label:        label,
		Subject:      cert.Subject.String(),
		CommonName:   cert.Subject.CommonName,
		Organization: org,
		SerialNumber: cert.SerialNumber.String(),
		NotBefore:    cert.NotBefore.UTC(),
		NotAfter:     cert.NotAfter.UTC(),
		SHA256:       strings.ToUpper(hex.EncodeToString(sum[:])),
		RegisteredAt: time.Now().UTC(),
	}
}
