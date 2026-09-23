package cryptoengine

import (
	"bytes"
	"context"
	"crypto/sha256"
)

const FixtureProviderName = "fixture"

func init() {
	RegisterProvider(FixtureProviderName, func() (Engine, error) { return Fixture{}, nil })
}

// Fixture is deterministic and intentionally not production cryptography. It
// exists for tests and for API/package plumbing before the production provider
// from CRYPTO-00 is selected.
type Fixture struct{}

func (Fixture) EncryptForRecipients(_ context.Context, plaintext []byte, recipients []RecipientRef) (Package, error) {
	if len(recipients) == 0 {
		return Package{}, ErrInvalidRecipient
	}
	ciphertext := xor(plaintext, 0x5a)
	hash := sha256.Sum256(plaintext)
	envelopes := make([]RecipientEnvelope, 0, len(recipients))
	for _, recipient := range recipients {
		if recipient.ID == "" {
			return Package{}, ErrInvalidRecipient
		}
		wrapped := sha256.Sum256([]byte("fixture:" + recipient.ID + ":" + recipient.CertificateID))
		envelopes = append(envelopes, RecipientEnvelope{RecipientID: recipient.ID, WrappedKey: wrapped[:]})
	}
	return Package{
		Version: 1, Provider: FixtureProviderName, Algorithm: "fixture-xor-sha256", Schema: "pkg.v1",
		PayloadHash: hash[:], Ciphertext: ciphertext, Recipients: envelopes,
	}, nil
}

func (Fixture) Decrypt(_ context.Context, pkg Package, recipient RecipientSecretRef) ([]byte, error) {
	if pkg.Provider != FixtureProviderName || pkg.Version != 1 || len(pkg.Ciphertext) == 0 {
		return nil, ErrInvalidPackage
	}
	if recipient.ID == "" {
		return nil, ErrInvalidRecipient
	}
	allowed := false
	for _, envelope := range pkg.Recipients {
		if envelope.RecipientID == recipient.ID {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrInvalidRecipient
	}
	plaintext := xor(pkg.Ciphertext, 0x5a)
	hash := sha256.Sum256(plaintext)
	if !bytes.Equal(hash[:], pkg.PayloadHash) {
		return nil, ErrInvalidPackage
	}
	return plaintext, nil
}

func (Fixture) Sign(_ context.Context, data []byte, signer SignerRef) (Signature, error) {
	if signer.ID == "" || signer.CertificateID == "" {
		return Signature{}, ErrInvalidSignature
	}
	value := fixtureSignature(data, signer.ID, signer.CertificateID)
	return Signature{Provider: FixtureProviderName, Algorithm: "fixture-sha256", SignerID: signer.ID, CertificateID: signer.CertificateID, Value: value}, nil
}

func (Fixture) Verify(_ context.Context, data []byte, signature Signature) (SignerInfo, error) {
	if signature.Provider != FixtureProviderName || signature.Algorithm != "fixture-sha256" {
		return SignerInfo{}, ErrInvalidSignature
	}
	expected := fixtureSignature(data, signature.SignerID, signature.CertificateID)
	if !bytes.Equal(expected, signature.Value) {
		return SignerInfo{}, ErrInvalidSignature
	}
	return SignerInfo{SignerID: signature.SignerID, CertificateID: signature.CertificateID, Provider: signature.Provider, Algorithm: signature.Algorithm}, nil
}

func fixtureSignature(data []byte, signerID, certificateID string) []byte {
	hash := sha256.New()
	hash.Write([]byte("fixture-signature:"))
	hash.Write([]byte(signerID))
	hash.Write([]byte(":"))
	hash.Write([]byte(certificateID))
	hash.Write([]byte(":"))
	hash.Write(data)
	return hash.Sum(nil)
}

func xor(input []byte, key byte) []byte {
	out := make([]byte, len(input))
	for i, value := range input {
		out[i] = value ^ key
	}
	return out
}
