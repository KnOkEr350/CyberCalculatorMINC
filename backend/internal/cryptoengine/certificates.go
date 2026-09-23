package cryptoengine

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrCertificateInvalid = errors.New("certificate metadata is invalid")
	ErrCertificateExpired = errors.New("certificate is not valid at the requested time")
	ErrCertificateRevoked = errors.New("certificate is revoked")
	ErrKeyUsageDenied     = errors.New("certificate key usage is not allowed")
	ErrUntrustedChain     = errors.New("certificate chain is not trusted")
)

type CertificateStatus string

const (
	CertificateActive    CertificateStatus = "active"
	CertificateSuspended CertificateStatus = "suspended"
	CertificateRevoked   CertificateStatus = "revoked"
	CertificateExpired   CertificateStatus = "expired"
)

type KeyUsage string

const (
	KeyUsageEncrypt KeyUsage = "encrypt"
	KeyUsageSign    KeyUsage = "sign"
	KeyUsageVerify  KeyUsage = "verify"
)

// CertificateRef is the persisted/public side of a key pair. It intentionally
// contains public certificate/key references and lifecycle metadata only.
type CertificateRef struct {
	ID               string
	OrganizationID   string
	RepresentativeID string
	PublicKeyID      string
	CertificateID    string
	IssuerID         string
	Status           CertificateStatus
	ValidFrom        time.Time
	ValidUntil       time.Time
	RevokedAt        *time.Time
	ReplacedByID     string
	Usages           []KeyUsage
}

type SignerMetadata struct {
	SignerID         string `json:"signer_id"`
	OrganizationID   string `json:"organization_id"`
	RepresentativeID string `json:"representative_id,omitempty"`
	CertificateID    string `json:"certificate_id"`
	PublicKeyID      string `json:"public_key_id"`
}

func (cert CertificateRef) ValidatePublicModel() error {
	if cert.ID == "" || cert.OrganizationID == "" || cert.CertificateID == "" || cert.PublicKeyID == "" {
		return fmt.Errorf("%w: id, organization, certificate and public key are required", ErrCertificateInvalid)
	}
	if cert.ValidFrom.IsZero() || cert.ValidUntil.IsZero() || cert.ValidUntil.Before(cert.ValidFrom) {
		return fmt.Errorf("%w: invalid validity period", ErrCertificateInvalid)
	}
	switch cert.Status {
	case CertificateActive, CertificateSuspended, CertificateRevoked, CertificateExpired:
	default:
		return fmt.Errorf("%w: unknown status %q", ErrCertificateInvalid, cert.Status)
	}
	if cert.Status == CertificateRevoked && cert.RevokedAt == nil {
		return fmt.Errorf("%w: revoked certificate requires revoked_at", ErrCertificateInvalid)
	}
	if len(cert.Usages) == 0 {
		return fmt.Errorf("%w: at least one key usage is required", ErrCertificateInvalid)
	}
	for _, usage := range cert.Usages {
		if usage != KeyUsageEncrypt && usage != KeyUsageSign && usage != KeyUsageVerify {
			return fmt.Errorf("%w: unknown usage %q", ErrCertificateInvalid, usage)
		}
	}
	return nil
}

func (cert CertificateRef) Allows(usage KeyUsage) bool {
	for _, candidate := range cert.Usages {
		if candidate == usage {
			return true
		}
	}
	return false
}

func (cert CertificateRef) SignerMetadata() SignerMetadata {
	return SignerMetadata{
		SignerID: cert.ID, OrganizationID: cert.OrganizationID,
		RepresentativeID: cert.RepresentativeID, CertificateID: cert.CertificateID, PublicKeyID: cert.PublicKeyID,
	}
}

func RotateCertificate(current, replacement CertificateRef, now time.Time) (CertificateRef, CertificateRef, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := replacement.ValidatePublicModel(); err != nil {
		return current, replacement, err
	}
	if current.ID == "" || replacement.ID == "" || current.ID == replacement.ID {
		return current, replacement, fmt.Errorf("%w: rotation requires two different certificate ids", ErrCertificateInvalid)
	}
	current.Status = CertificateRevoked
	current.RevokedAt = &now
	current.ReplacedByID = replacement.ID
	replacement.Status = CertificateActive
	return current, replacement, nil
}
