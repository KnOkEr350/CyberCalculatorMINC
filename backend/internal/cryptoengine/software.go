package cryptoengine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

// SoftwareProviderName — провайдер на стандартной библиотеке Go. Это рабочая
// реализация интерфейса Engine, но не производственный профиль: выбор
// провайдера и допустимых видов подписи для поставки остаётся решением CRYPTO-00
// (КриптоПро или иной сертифицированный провайдер подключается через тот же
// интерфейс и не меняет форматы, построенные поверх него).
const (
	SoftwareProviderName       = "software"
	SoftwareCipherAlgorithm    = "x25519-hkdf-sha256+aes-256-gcm"
	SoftwareSignatureAlgorithm = "ed25519"

	softwareVersion = 1
	x25519KeySize   = 32
	gcmNonceSize    = 12
	// Обёрнутый ключ: эфемерный открытый ключ + nonce + зашифрованный ключ
	// содержимого с тегом аутентификации.
	wrappedKeySize = x25519KeySize + gcmNonceSize + 32 + 16

	aeadContext = "cybercalc-pkg-v1/content"
	kdfInfo     = "cybercalc-pkg-v1/key-wrap"
)

// KeyStore отдаёт закрытые ключи по ссылке. Сами ключи не покидают хранилище:
// в Engine, БД и HTTP передаются только ссылки (RecipientSecretRef.KeyRef и
// SignerRef.KeyRef).
type KeyStore interface {
	DecryptionKey(ref string) (*ecdh.PrivateKey, error)
	SigningKey(ref string) (ed25519.PrivateKey, error)
}

// VerificationKeys отдаёт открытый ключ проверки подписи по идентификатору
// сертификата. Цепочка, срок и отзыв сертификата проверяются отдельно
// SignaturePolicy: здесь только математика подписи.
type VerificationKeys interface {
	VerificationKey(certificateID string) (ed25519.PublicKey, error)
}

var (
	ErrUnknownKey = errors.New("key reference is not known to the key store")
)

// Software реализует Engine: X25519 + HKDF-SHA256 для обёртки ключа получателя,
// AES-256-GCM для содержимого и Ed25519 для подписи.
type Software struct {
	keys   KeyStore
	verify VerificationKeys
}

func NewSoftware(keys KeyStore, verify VerificationKeys) *Software {
	return &Software{keys: keys, verify: verify}
}

func (s *Software) EncryptForRecipients(ctx context.Context, plaintext []byte, recipients []RecipientRef) (Package, error) {
	if err := ctx.Err(); err != nil {
		return Package{}, err
	}
	if len(recipients) == 0 {
		return Package{}, ErrInvalidRecipient
	}
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if recipient.ID == "" || seen[recipient.ID] {
			return Package{}, fmt.Errorf("%w: пустой или повторяющийся получатель", ErrInvalidRecipient)
		}
		seen[recipient.ID] = true
	}

	contentKey := make([]byte, 32)
	if _, err := rand.Read(contentKey); err != nil {
		return Package{}, err
	}
	aead, err := newGCM(contentKey)
	if err != nil {
		return Package{}, err
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return Package{}, err
	}
	ciphertext := append(nonce, aead.Seal(nil, nonce, plaintext, []byte(aeadContext))...)

	envelopes := make([]RecipientEnvelope, 0, len(recipients))
	for _, recipient := range recipients {
		wrapped, err := wrapKey(contentKey, recipient)
		if err != nil {
			return Package{}, err
		}
		envelopes = append(envelopes, RecipientEnvelope{RecipientID: recipient.ID, WrappedKey: wrapped})
	}
	hash := sha256.Sum256(plaintext)
	return Package{
		Version: softwareVersion, Provider: SoftwareProviderName, Algorithm: SoftwareCipherAlgorithm,
		Schema: "pkg.v1", PayloadHash: hash[:], Ciphertext: ciphertext, Recipients: envelopes,
	}, nil
}

func (s *Software) Decrypt(ctx context.Context, pkg Package, recipient RecipientSecretRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Провайдер, версия и алгоритм — жёсткая проверка: пакет со слабым
	// алгоритмом не должен расшифровываться «как получится».
	if pkg.Provider != SoftwareProviderName || pkg.Version != softwareVersion || pkg.Algorithm != SoftwareCipherAlgorithm {
		return nil, ErrInvalidPackage
	}
	if recipient.ID == "" || recipient.KeyRef == "" {
		return nil, ErrInvalidRecipient
	}
	if len(pkg.Ciphertext) < gcmNonceSize+16 {
		return nil, ErrInvalidPackage
	}
	var envelope *RecipientEnvelope
	for i := range pkg.Recipients {
		if pkg.Recipients[i].RecipientID == recipient.ID {
			envelope = &pkg.Recipients[i]
			break
		}
	}
	if envelope == nil {
		return nil, ErrInvalidRecipient
	}
	private, err := s.keys.DecryptionKey(recipient.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRecipient, err)
	}
	contentKey, err := unwrapKey(envelope.WrappedKey, recipient.ID, private)
	if err != nil {
		return nil, ErrInvalidRecipient
	}
	aead, err := newGCM(contentKey)
	if err != nil {
		return nil, ErrInvalidPackage
	}
	nonce, sealed := pkg.Ciphertext[:gcmNonceSize], pkg.Ciphertext[gcmNonceSize:]
	plaintext, err := aead.Open(nil, nonce, sealed, []byte(aeadContext))
	if err != nil {
		return nil, ErrInvalidPackage
	}
	hash := sha256.Sum256(plaintext)
	if !bytes.Equal(hash[:], pkg.PayloadHash) {
		return nil, ErrInvalidPackage
	}
	return plaintext, nil
}

func (s *Software) Sign(ctx context.Context, data []byte, signer SignerRef) (Signature, error) {
	if err := ctx.Err(); err != nil {
		return Signature{}, err
	}
	if signer.ID == "" || signer.CertificateID == "" || signer.KeyRef == "" {
		return Signature{}, ErrInvalidSignature
	}
	key, err := s.keys.SigningKey(signer.KeyRef)
	if err != nil {
		return Signature{}, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	return Signature{
		Provider: SoftwareProviderName, Algorithm: SoftwareSignatureAlgorithm,
		SignerID: signer.ID, CertificateID: signer.CertificateID, Value: ed25519.Sign(key, data),
	}, nil
}

func (s *Software) Verify(ctx context.Context, data []byte, signature Signature) (SignerInfo, error) {
	if err := ctx.Err(); err != nil {
		return SignerInfo{}, err
	}
	if signature.Provider != SoftwareProviderName || signature.Algorithm != SoftwareSignatureAlgorithm {
		return SignerInfo{}, ErrInvalidSignature
	}
	if len(signature.Value) != ed25519.SignatureSize {
		return SignerInfo{}, ErrInvalidSignature
	}
	public, err := s.verify.VerificationKey(signature.CertificateID)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return SignerInfo{}, ErrInvalidSignature
	}
	if !ed25519.Verify(public, data, signature.Value) {
		return SignerInfo{}, ErrInvalidSignature
	}
	return SignerInfo{
		SignerID: signature.SignerID, CertificateID: signature.CertificateID,
		Provider: signature.Provider, Algorithm: signature.Algorithm,
	}, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// wrapKey оборачивает ключ содержимого для одного получателя. У каждого
// получателя своя эфемерная пара ключей, а идентификатор получателя входит в
// аутентифицируемые данные: конверт нельзя переложить другому получателю.
func wrapKey(contentKey []byte, recipient RecipientRef) ([]byte, error) {
	public, err := ecdh.X25519().NewPublicKey(recipient.PublicKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: некорректный открытый ключ получателя %q", ErrInvalidRecipient, recipient.ID)
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := ephemeral.ECDH(public)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRecipient, err)
	}
	kek, err := deriveKey(shared, ephemeral.PublicKey().Bytes(), public.Bytes())
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{}, ephemeral.PublicKey().Bytes()...)
	out = append(out, nonce...)
	return append(out, aead.Seal(nil, nonce, contentKey, []byte(recipient.ID))...), nil
}

func unwrapKey(wrapped []byte, recipientID string, private *ecdh.PrivateKey) ([]byte, error) {
	if private == nil || len(wrapped) != wrappedKeySize {
		return nil, ErrInvalidRecipient
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(wrapped[:x25519KeySize])
	if err != nil {
		return nil, err
	}
	shared, err := private.ECDH(ephemeral)
	if err != nil {
		return nil, err
	}
	kek, err := deriveKey(shared, wrapped[:x25519KeySize], private.PublicKey().Bytes())
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(kek)
	if err != nil {
		return nil, err
	}
	nonce := wrapped[x25519KeySize : x25519KeySize+gcmNonceSize]
	return aead.Open(nil, nonce, wrapped[x25519KeySize+gcmNonceSize:], []byte(recipientID))
}

func deriveKey(shared, ephemeralPublic, recipientPublic []byte) ([]byte, error) {
	salt := append(append([]byte{}, ephemeralPublic...), recipientPublic...)
	return hkdf.Key(sha256.New, shared, salt, kdfInfo, 32)
}

// MemoryKeyring — хранилище ключей в памяти процесса для тестов и локальной
// разработки. В рабочем контуре ключи лежат в хранилище КриптоПро/HSM, а
// приложение видит только ссылки.
type MemoryKeyring struct {
	decrypt map[string]*ecdh.PrivateKey
	sign    map[string]ed25519.PrivateKey
	verify  map[string]ed25519.PublicKey
}

func NewMemoryKeyring() *MemoryKeyring {
	return &MemoryKeyring{
		decrypt: map[string]*ecdh.PrivateKey{},
		sign:    map[string]ed25519.PrivateKey{},
		verify:  map[string]ed25519.PublicKey{},
	}
}

// NewRecipient создаёт пару ключей получателя под ссылкой ref и возвращает
// открытый ключ для RecipientRef.
func (k *MemoryKeyring) NewRecipient(ref string) ([]byte, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	k.decrypt[ref] = key
	return key.PublicKey().Bytes(), nil
}

// NewSigner создаёт пару ключей подписи: закрытый ключ остаётся под ссылкой
// ref, открытый становится ключом проверки сертификата certificateID.
func (k *MemoryKeyring) NewSigner(ref, certificateID string) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	k.sign[ref] = private
	k.verify[certificateID] = public
	return nil
}

func (k *MemoryKeyring) DecryptionKey(ref string) (*ecdh.PrivateKey, error) {
	if key, ok := k.decrypt[ref]; ok {
		return key, nil
	}
	return nil, ErrUnknownKey
}

func (k *MemoryKeyring) SigningKey(ref string) (ed25519.PrivateKey, error) {
	if key, ok := k.sign[ref]; ok {
		return key, nil
	}
	return nil, ErrUnknownKey
}

func (k *MemoryKeyring) VerificationKey(certificateID string) (ed25519.PublicKey, error) {
	if key, ok := k.verify[certificateID]; ok {
		return key, nil
	}
	return nil, ErrUnknownKey
}
