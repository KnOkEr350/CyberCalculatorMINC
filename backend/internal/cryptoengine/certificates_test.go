package cryptoengine

import (
	"errors"
	"testing"
	"time"
)

func certificateFixture(id string, usages ...KeyUsage) CertificateRef {
	return CertificateRef{
		ID: id, OrganizationID: "org-1", PublicKeyID: "pub-" + id, CertificateID: "cert-" + id,
		Status: CertificateActive, ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ValidUntil: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC), Usages: usages,
	}
}

func TestCertificatePublicModelAndRotation(t *testing.T) {
	current := certificateFixture("old", KeyUsageVerify)
	replacement := certificateFixture("new", KeyUsageVerify)
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	rotated, active, err := RotateCertificate(current, replacement, now)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Status != CertificateRevoked || rotated.RevokedAt == nil || !rotated.RevokedAt.Equal(now) || rotated.ReplacedByID != "new" {
		t.Fatalf("old certificate rotation metadata invalid: %+v", rotated)
	}
	if active.Status != CertificateActive {
		t.Fatalf("replacement certificate should be active: %+v", active)
	}
	if err := (CertificateRef{ID: "bad"}).ValidatePublicModel(); !errors.Is(err, ErrCertificateInvalid) {
		t.Fatalf("incomplete certificate error = %v", err)
	}
}

func TestSignaturePolicyValidatesChainTimeRevocationAndUsage(t *testing.T) {
	root := certificateFixture("root", KeyUsageVerify)
	leaf := certificateFixture("leaf", KeyUsageVerify)
	leaf.IssuerID = root.ID
	policy := SignaturePolicy{
		Now:            time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		TrustedRootIDs: map[string]bool{"root": true},
		RequiredUsage:  KeyUsageVerify,
	}
	info, err := policy.ValidateCertificateChain([]CertificateRef{leaf, root})
	if err != nil {
		t.Fatal(err)
	}
	if info.CertificateID != "cert-leaf" || info.OrganizationID != "org-1" {
		t.Fatalf("signer metadata invalid: %+v", info)
	}
	policy.RevokedCertificateIDs = map[string]bool{"leaf": true}
	if _, err := policy.ValidateCertificateChain([]CertificateRef{leaf, root}); !errors.Is(err, ErrCertificateRevoked) {
		t.Fatalf("revoked certificate error = %v", err)
	}
	policy.RevokedCertificateIDs = nil
	policy.Now = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := policy.ValidateCertificateChain([]CertificateRef{leaf, root}); !errors.Is(err, ErrCertificateExpired) {
		t.Fatalf("expired certificate error = %v", err)
	}
	policy.Now = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	policy.RequiredUsage = KeyUsageEncrypt
	if _, err := policy.ValidateCertificateChain([]CertificateRef{leaf, root}); !errors.Is(err, ErrKeyUsageDenied) {
		t.Fatalf("usage error = %v", err)
	}
	policy.RequiredUsage = KeyUsageVerify
	leaf.IssuerID = "unknown"
	if _, err := policy.ValidateCertificateChain([]CertificateRef{leaf, root}); !errors.Is(err, ErrUntrustedChain) {
		t.Fatalf("chain error = %v", err)
	}
}

func TestCanonicalJSONSortsKeysRecursively(t *testing.T) {
	got, err := CanonicalJSON([]byte(`{"b":2,"a":{"d":4,"c":[3,2,1]}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"c":[3,2,1],"d":4},"b":2}`
	if string(got) != want {
		t.Fatalf("CanonicalJSON() = %s, want %s", got, want)
	}
}
