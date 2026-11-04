package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sml/sml-signx/internal/store"
)

func TestRegisterLookupRemoveAndPersist(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	meta := store.Certificate{
		Path:         filepath.Join(dir, "company.pfx"),
		Label:        "Company",
		Subject:      "CN=Company, O=SML",
		CommonName:   "Company",
		Organization: "SML",
		SerialNumber: "42",
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		SHA256:       "ABC",
	}
	if err := st.Register(meta); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, ok := st.Lookup(meta.Path)
	if !ok {
		t.Fatal("lookup did not find registered certificate")
	}
	if got.Label != "Company" {
		t.Fatalf("label = %q, want Company", got.Label)
	}

	// Registering the same path should update, not duplicate.
	meta.Label = "Company Updated"
	if err := st.Register(meta); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if n := len(st.List()); n != 1 {
		t.Fatalf("list length = %d, want 1", n)
	}

	// Re-open to verify persistence.
	st2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok = st2.Lookup(meta.Path)
	if !ok || got.Label != "Company Updated" {
		t.Fatalf("persisted entry = %+v, found=%v", got, ok)
	}

	removed, err := st2.Remove(meta.Path)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed {
		t.Fatal("remove reported false")
	}
	if _, ok := st2.Lookup(meta.Path); ok {
		t.Fatal("entry still present after remove")
	}
}

func TestCertificateExpired(t *testing.T) {
	c := store.Certificate{NotAfter: time.Now().Add(-time.Minute)}
	if !c.Expired(time.Now()) {
		t.Fatal("expected certificate to be expired")
	}
	c.NotAfter = time.Now().Add(time.Minute)
	if c.Expired(time.Now()) {
		t.Fatal("expected certificate to be valid")
	}
}
