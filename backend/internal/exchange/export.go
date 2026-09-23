package exchange

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"cybercalc/internal/cryptoengine"
)

// ExportRequest — что и кому отправляется.
type ExportRequest struct {
	OrganizationID string
	// Signer — представитель отправителя. Закрытый ключ здесь не передаётся:
	// подписывает провайдер по ссылке на ключ.
	Signer     cryptoengine.SignerRef
	Recipients []cryptoengine.RecipientRef
	Records    []Record
	ReportYear int
	Period     string
	Now        time.Time
}

type payloadDocument struct {
	Records []Record `json:"records"`
}

// Export собирает пакет: записи → JSON → gzip → шифрование для каждого
// получателя → подпись манифеста представителем отправителя.
//
// Открытый текст существует только в памяти этого вызова и сразу уходит на
// шифрование: ни временных файлов, ни журналов с содержимым нет. Ключ
// содержимого один, но обёрнут отдельно для каждого получателя — пакет
// открывает только тот, кому он адресован.
func Export(ctx context.Context, engine cryptoengine.Engine, request ExportRequest) ([]byte, error) {
	if err := validateExport(request); err != nil {
		return nil, err
	}
	records, err := sortedRecords(request.Records)
	if err != nil {
		return nil, err
	}
	plaintext, err := json.Marshal(payloadDocument{Records: records})
	if err != nil {
		return nil, err
	}
	if len(plaintext) > MaxPlaintextBytes {
		return nil, fmt.Errorf("%w: слишком много данных для одного пакета", ErrInvalidExportRequest)
	}
	compressed, err := compress(plaintext)
	if err != nil {
		return nil, err
	}
	encrypted, err := engine.EncryptForRecipients(ctx, compressed, request.Recipients)
	if err != nil {
		return nil, err
	}

	certificates := make(map[string]string, len(request.Recipients))
	for _, recipient := range request.Recipients {
		certificates[recipient.ID] = recipient.CertificateID
	}
	recipients := make([]ManifestRecipient, 0, len(encrypted.Recipients))
	for _, envelope := range encrypted.Recipients {
		recipients = append(recipients, ManifestRecipient{
			ID: envelope.RecipientID, CertificateID: certificates[envelope.RecipientID], WrappedKey: envelope.WrappedKey,
		})
	}
	sort.Slice(recipients, func(i, j int) bool { return recipients[i].ID < recipients[j].ID })

	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}
	ciphertextHash := sha256.Sum256(encrypted.Ciphertext)
	manifest := Manifest{
		Format: FormatName, Version: FormatVersion, Schema: SchemaRecords,
		CreatedAt: now.UTC().Truncate(time.Second),
		Sender: ManifestSender{
			OrganizationID: request.OrganizationID, SignerID: request.Signer.ID, CertificateID: request.Signer.CertificateID,
		},
		Recipients: recipients,
		Cipher: ManifestCipher{
			Provider: encrypted.Provider, Algorithm: encrypted.Algorithm, Compression: CompressionGzip,
			PayloadSHA256: hex.EncodeToString(encrypted.PayloadHash), CiphertextSHA256: hex.EncodeToString(ciphertextHash[:]),
			PlaintextSize: len(compressed),
		},
		ReportYear: request.ReportYear, Period: request.Period, RecordCount: len(records),
	}
	canonical, err := canonicalManifest(manifest)
	if err != nil {
		return nil, err
	}
	signature, err := engine.Sign(ctx, canonical, request.Signer)
	if err != nil {
		return nil, err
	}
	return assemble(canonical, signature, encrypted.Ciphertext)
}

func validateExport(request ExportRequest) error {
	fail := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidExportRequest, reason) }
	if request.OrganizationID == "" {
		return fail("не указана организация отправителя")
	}
	if request.Signer.ID == "" || request.Signer.CertificateID == "" || request.Signer.KeyRef == "" {
		return fail("не указан представитель, его сертификат или ссылка на ключ подписи")
	}
	if len(request.Recipients) == 0 || len(request.Recipients) > MaxRecipients {
		return fail(fmt.Sprintf("получателей должно быть от 1 до %d", MaxRecipients))
	}
	seen := map[string]bool{}
	for _, recipient := range request.Recipients {
		if recipient.ID == "" || recipient.CertificateID == "" || len(recipient.PublicKeyBytes) == 0 {
			return fail("у получателя должны быть идентификатор, сертификат и открытый ключ")
		}
		if seen[recipient.ID] {
			return fail("получатель указан дважды")
		}
		seen[recipient.ID] = true
	}
	if request.ReportYear < 2000 || request.ReportYear > 2100 {
		return fail("некорректный отчётный год")
	}
	if request.Period != "plan" && request.Period != "fact" {
		return fail("период должен быть plan или fact")
	}
	if len(request.Records) == 0 {
		return fail("в пакете нет записей")
	}
	if len(request.Records) > MaxRecords {
		return fail(fmt.Sprintf("в одном пакете не более %d записей", MaxRecords))
	}
	return nil
}

// sortedRecords упорядочивает записи по ключу: один и тот же набор всегда даёт
// один и тот же открытый текст. Заодно отсекаются записи без ключа и
// дубликаты — отправлять данные, которые получатель не сможет сопоставить,
// бессмысленно.
func sortedRecords(records []Record) ([]Record, error) {
	type keyed struct {
		id     string
		record Record
	}
	items := make([]keyed, 0, len(records))
	seen := map[string]bool{}
	for _, record := range records {
		key, err := KeyOf(record)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidExportRequest, err)
		}
		id := key.String()
		if seen[id] {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateKey, id)
		}
		seen[id] = true
		items = append(items, keyed{id: id, record: record})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })
	out := make([]Record, len(items))
	for i, item := range items {
		out[i] = item.record
	}
	return out, nil
}

func compress(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
