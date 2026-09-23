package exchange

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"cybercalc/internal/cryptoengine"
)

// ChainSource отдаёт цепочку сертификатов подписанта от конечного
// сертификата к корню.
type ChainSource interface {
	Chain(ctx context.Context, certificateID string) ([]cryptoengine.CertificateRef, error)
}

// ImportPolicy — что получатель считает допустимым.
type ImportPolicy struct {
	// Recipient — кто открывает пакет; закрытый ключ берётся по ссылке.
	Recipient cryptoengine.RecipientSecretRef
	// Signature — срок, доверенные корни и отозванные сертификаты.
	Signature cryptoengine.SignaturePolicy
	Chains    ChainSource
	// AllowedCiphers и AllowedSignatures — закрытые списки «провайдер/алгоритм».
	// Пакет с любым другим алгоритмом отклоняется: понизить стойкость подменой
	// заголовка нельзя.
	AllowedCiphers    map[string]bool
	AllowedSignatures map[string]bool
	// ExpectedYear и ExpectedPeriod, если заданы, отсекают пакет за другой год
	// или период ещё до расшифровки.
	ExpectedYear   int
	ExpectedPeriod string
}

// CipherKey и SignatureKey — ключи разрешённых списков.
func CipherKey(provider, algorithm string) string { return provider + "/" + algorithm }

// Imported — проверенное содержимое пакета. БД к этому моменту не тронута:
// применять результат или нет, решает оператор после просмотра различий.
type Imported struct {
	Manifest Manifest
	Signer   cryptoengine.SignerMetadata
	Records  []Record
}

// Open проверяет и открывает пакет. Порядок важен и не меняется: всё, что
// можно проверить без ключа получателя, проверяется до расшифровки, а
// содержимое разбирается только после того, как подпись, цепочка сертификатов
// и целостность подтверждены.
func Open(ctx context.Context, engine cryptoengine.Engine, policy ImportPolicy, data []byte) (Imported, error) {
	if policy.Chains == nil {
		return Imported{}, fmt.Errorf("%w: не задан источник цепочек сертификатов", cryptoengine.ErrUntrustedChain)
	}
	// 1. Форма файла и размеры.
	parsed, err := parseContainer(data)
	if err != nil {
		return Imported{}, err
	}
	// 2. Манифест: каноническая запись, формат, версия, схема.
	manifest, err := parseManifest(parsed.manifestBytes)
	if err != nil {
		return Imported{}, err
	}
	if manifest.Format != FormatName || manifest.Version != FormatVersion {
		return Imported{}, ErrUnsupportedVersion
	}
	if manifest.Schema != SchemaRecords {
		return Imported{}, ErrSchemaMismatch
	}
	if manifest.Cipher.Compression != CompressionGzip {
		return Imported{}, fmt.Errorf("%w: сжатие %q", ErrAlgorithmNotAllowed, manifest.Cipher.Compression)
	}
	if len(manifest.Recipients) == 0 || len(manifest.Recipients) > MaxRecipients ||
		manifest.RecordCount < 0 || manifest.RecordCount > MaxRecords {
		return Imported{}, ErrPackageFormat
	}
	// 3. Алгоритмы — только из закрытых списков.
	if !policy.AllowedCiphers[CipherKey(manifest.Cipher.Provider, manifest.Cipher.Algorithm)] {
		return Imported{}, fmt.Errorf("%w: шифр %s/%s", ErrAlgorithmNotAllowed, manifest.Cipher.Provider, manifest.Cipher.Algorithm)
	}
	if !policy.AllowedSignatures[CipherKey(parsed.signature.Provider, parsed.signature.Algorithm)] {
		return Imported{}, fmt.Errorf("%w: подпись %s/%s", ErrAlgorithmNotAllowed, parsed.signature.Provider, parsed.signature.Algorithm)
	}
	if policy.ExpectedYear != 0 && manifest.ReportYear != policy.ExpectedYear {
		return Imported{}, fmt.Errorf("%w: пакет за %d год", ErrSchemaMismatch, manifest.ReportYear)
	}
	if policy.ExpectedPeriod != "" && manifest.Period != policy.ExpectedPeriod {
		return Imported{}, fmt.Errorf("%w: пакет за период %q", ErrSchemaMismatch, manifest.Period)
	}
	// 4. Целостность шифртекста относительно подписанного манифеста.
	sum := sha256.Sum256(parsed.ciphertext)
	if hex.EncodeToString(sum[:]) != manifest.Cipher.CiphertextSHA256 {
		return Imported{}, ErrTampered
	}
	// 5. Подпись манифеста.
	if _, err := engine.Verify(ctx, parsed.manifestBytes, parsed.signature); err != nil {
		return Imported{}, err
	}
	// 6. Цепочка сертификатов: срок, отзыв, назначение, доверенный корень.
	if parsed.signature.CertificateID != manifest.Sender.CertificateID || parsed.signature.SignerID != manifest.Sender.SignerID {
		return Imported{}, ErrSenderMismatch
	}
	chain, err := policy.Chains.Chain(ctx, manifest.Sender.CertificateID)
	if err != nil {
		return Imported{}, err
	}
	signer, err := policy.Signature.ValidateCertificateChain(chain)
	if err != nil {
		return Imported{}, err
	}
	// Организация, названная в манифесте, должна быть владельцем сертификата:
	// иначе подписант мог бы выдать пакет за чужой.
	if signer.OrganizationID != manifest.Sender.OrganizationID || signer.SignerID != manifest.Sender.SignerID {
		return Imported{}, ErrSenderMismatch
	}
	// 7. Пакет адресован нам.
	var mine *ManifestRecipient
	for i := range manifest.Recipients {
		if manifest.Recipients[i].ID == policy.Recipient.ID {
			mine = &manifest.Recipients[i]
			break
		}
	}
	if mine == nil {
		return Imported{}, ErrNotARecipient
	}
	// 8. Расшифровка. Только теперь в ход идёт закрытый ключ получателя.
	payloadHash, err := hex.DecodeString(manifest.Cipher.PayloadSHA256)
	if err != nil {
		return Imported{}, ErrPackageFormat
	}
	envelopes := make([]cryptoengine.RecipientEnvelope, 0, len(manifest.Recipients))
	for _, recipient := range manifest.Recipients {
		envelopes = append(envelopes, cryptoengine.RecipientEnvelope{RecipientID: recipient.ID, WrappedKey: recipient.WrappedKey})
	}
	compressed, err := engine.Decrypt(ctx, cryptoengine.Package{
		Version: 1, Provider: manifest.Cipher.Provider, Algorithm: manifest.Cipher.Algorithm, Schema: "pkg.v1",
		PayloadHash: payloadHash, Ciphertext: parsed.ciphertext, Recipients: envelopes,
	}, policy.Recipient)
	if err != nil {
		return Imported{}, err
	}
	// 9. Распаковка с жёстким пределом и только после этого — разбор JSON.
	plaintext, err := decompress(compressed)
	if err != nil {
		return Imported{}, err
	}
	var document payloadDocument
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Imported{}, fmt.Errorf("%w: содержимое: %v", ErrPackageFormat, err)
	}
	if decoder.More() {
		return Imported{}, fmt.Errorf("%w: лишние данные после содержимого", ErrPackageFormat)
	}
	if len(document.Records) != manifest.RecordCount {
		return Imported{}, ErrRecordCountMismatch
	}
	for _, record := range document.Records {
		if _, err := KeyOf(record); err != nil {
			return Imported{}, fmt.Errorf("%w: %v", ErrPackageFormat, err)
		}
	}
	return Imported{Manifest: manifest, Signer: signer, Records: document.Records}, nil
}

// decompress читает не больше предела: сжатые данные, разворачивающиеся в
// гигабайты, останавливаются на границе, а не в памяти сервера.
func decompress(data []byte) ([]byte, error) {
	limit := plaintextLimit
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, ErrPackageFormat
	}
	defer reader.Close()
	limited := io.LimitReader(reader, int64(limit)+1)
	out, err := io.ReadAll(limited)
	if err != nil {
		return nil, ErrPackageFormat
	}
	if len(out) > limit {
		return nil, ErrDecompressionLimit
	}
	return out, nil
}
