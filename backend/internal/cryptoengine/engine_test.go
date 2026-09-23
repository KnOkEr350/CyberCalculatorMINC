package cryptoengine

import (
	"context"
	"errors"
	"testing"
)

func TestFixtureProviderRoundTripAndSignature(t *testing.T) {
	engine, err := New(FixtureProviderName)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pkg, err := engine.EncryptForRecipients(ctx, []byte("payload"), []RecipientRef{{ID: "mfti", CertificateID: "cert-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(pkg.Ciphertext) == "payload" {
		t.Fatal("fixture package must not store plaintext in ciphertext")
	}
	plain, err := engine.Decrypt(ctx, pkg, RecipientSecretRef{ID: "mfti", KeyRef: "keystore://mfti"})
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "payload" {
		t.Fatalf("decrypt() = %q", plain)
	}
	if _, err := engine.Decrypt(ctx, pkg, RecipientSecretRef{ID: "other"}); !errors.Is(err, ErrInvalidRecipient) {
		t.Fatalf("wrong recipient error = %v", err)
	}
	signature, err := engine.Sign(ctx, []byte("payload"), SignerRef{ID: "company", CertificateID: "cert-company", KeyRef: "csp://company"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := engine.Verify(ctx, []byte("payload"), signature)
	if err != nil {
		t.Fatal(err)
	}
	if info.SignerID != "company" || info.CertificateID != "cert-company" {
		t.Fatalf("unexpected signer info: %+v", info)
	}
	if _, err := engine.Verify(ctx, []byte("tampered"), signature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("tampered signature error = %v", err)
	}
}

func TestProviderFactoryAndCryptoProStub(t *testing.T) {
	if _, err := New("missing"); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("unknown provider error = %v", err)
	}
	if _, err := New(CryptoProProvider); !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("cryptopro stub error = %v", err)
	}
}

func TestPrivateKeyMaterialRejectedAtBoundary(t *testing.T) {
	if err := ValidateNoPrivateKeyMaterial(map[string][]byte{"private_key": []byte("secret")}); !errors.Is(err, ErrPrivateKeyMaterial) {
		t.Fatalf("private key material error = %v", err)
	}
	if err := ValidateNoPrivateKeyMaterial(map[string][]byte{"private_key": nil}); err != nil {
		t.Fatalf("empty private material should be accepted: %v", err)
	}
}
