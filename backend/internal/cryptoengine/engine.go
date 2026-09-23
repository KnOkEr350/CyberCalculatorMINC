package cryptoengine

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const CryptoProProvider = "cryptopro_cgo"

var (
	ErrUnknownProvider     = errors.New("crypto provider is not registered")
	ErrProviderUnavailable = errors.New("crypto provider is unavailable")
	ErrInvalidRecipient    = errors.New("invalid package recipient")
	ErrInvalidPackage      = errors.New("invalid encrypted package")
	ErrInvalidSignature    = errors.New("invalid signature")
	ErrPrivateKeyMaterial  = errors.New("private key material must not be persisted or passed through CryptoEngine DTO")
)

type RecipientRef struct {
	ID             string
	CertificateID  string
	PublicKeyBytes []byte
}

// RecipientSecretRef deliberately stores only references to private material:
// the actual private key must stay in CSP/HSM/OS keystore and never flow
// through persistence, HTTP payloads or exchange package metadata.
type RecipientSecretRef struct {
	ID        string
	KeyRef    string
	PINHandle string
}

// SignerRef follows the same rule: the signer is identified by certificate or
// keystore reference, not by a private key blob.
type SignerRef struct {
	ID            string
	CertificateID string
	KeyRef        string
	PINHandle     string
}

type RecipientEnvelope struct {
	RecipientID string
	WrappedKey  []byte
}

type Package struct {
	Version     int
	Provider    string
	Algorithm   string
	Schema      string
	PayloadHash []byte
	Ciphertext  []byte
	Recipients  []RecipientEnvelope
}

type Signature struct {
	Provider      string
	Algorithm     string
	SignerID      string
	CertificateID string
	Value         []byte
}

type SignerInfo struct {
	SignerID      string
	CertificateID string
	Provider      string
	Algorithm     string
}

type Engine interface {
	EncryptForRecipients(ctx context.Context, plaintext []byte, recipients []RecipientRef) (Package, error)
	Decrypt(ctx context.Context, pkg Package, recipient RecipientSecretRef) ([]byte, error)
	Sign(ctx context.Context, data []byte, signer SignerRef) (Signature, error)
	Verify(ctx context.Context, data []byte, signature Signature) (SignerInfo, error)
}

type ProviderFactory func() (Engine, error)

var registry = struct {
	sync.RWMutex
	factories map[string]ProviderFactory
}{factories: map[string]ProviderFactory{}}

func RegisterProvider(name string, factory ProviderFactory) {
	registry.Lock()
	defer registry.Unlock()
	if name == "" || factory == nil {
		panic("crypto provider name and factory are required")
	}
	registry.factories[name] = factory
}

func New(provider string) (Engine, error) {
	registry.RLock()
	factory := registry.factories[provider]
	registry.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, provider)
	}
	return factory()
}

func ValidateNoPrivateKeyMaterial(fields map[string][]byte) error {
	for name, value := range fields {
		if len(value) > 0 {
			return fmt.Errorf("%w: %s", ErrPrivateKeyMaterial, name)
		}
	}
	return nil
}
