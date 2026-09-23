package exchange

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cybercalc/internal/cryptoengine"
)

// Формат пакета .pkg (CRYPTO-03).
//
//	magic      "CCPKG"           5 байт
//	version    формат            1 байт
//	manifest   uint32 длина + канонический JSON
//	signature  uint32 длина + JSON подписи манифеста
//	ciphertext uint64 длина + шифртекст (gzip-полезная нагрузка)
//
// Подписывается манифест, а в манифесте зафиксирован SHA-256 шифртекста: любое
// изменение любой части пакета ломает подпись ещё до расшифровки.
const (
	Magic         = "CCPKG"
	FormatVersion = 1
	FormatName    = "cybercalc.pkg"
	SchemaRecords = "exchange.records.v1"

	MaxPackageBytes   = 64 << 20  // весь файл
	MaxManifestBytes  = 1 << 20   // манифест
	MaxSignatureBytes = 64 << 10  // подпись
	MaxPlaintextBytes = 256 << 20 // после распаковки: защита от «бомбы»
	MaxRecords        = 200_000
	MaxRecipients     = 64

	CompressionGzip = "gzip"
)

// plaintextLimit — действующий предел распаковки. Переменная, а не константа,
// чтобы тест «бомбы» не разворачивал сотни мегабайт.
var plaintextLimit = MaxPlaintextBytes

var (
	ErrPackageFormat        = errors.New("файл не является пакетом обмена или повреждён")
	ErrUnsupportedVersion   = errors.New("версия формата пакета не поддерживается")
	ErrSchemaMismatch       = errors.New("схема данных пакета не поддерживается")
	ErrAlgorithmNotAllowed  = errors.New("алгоритм пакета не разрешён политикой")
	ErrPackageTooLarge      = errors.New("пакет превышает допустимый размер")
	ErrTampered             = errors.New("содержимое пакета не соответствует подписанному манифесту")
	ErrSenderMismatch       = errors.New("отправитель в манифесте не совпадает с владельцем сертификата подписи")
	ErrNotARecipient        = errors.New("пакет не адресован этому получателю")
	ErrDecompressionLimit   = errors.New("распакованное содержимое превышает допустимый размер")
	ErrRecordCountMismatch  = errors.New("число записей не совпадает с манифестом")
	ErrInvalidExportRequest = errors.New("некорректный запрос на экспорт")
)

// Manifest — подписываемое описание пакета.
type Manifest struct {
	Format      string              `json:"format"`
	Version     int                 `json:"version"`
	Schema      string              `json:"schema"`
	CreatedAt   time.Time           `json:"created_at"`
	Sender      ManifestSender      `json:"sender"`
	Recipients  []ManifestRecipient `json:"recipients"`
	Cipher      ManifestCipher      `json:"cipher"`
	ReportYear  int                 `json:"report_year"`
	Period      string              `json:"period"`
	RecordCount int                 `json:"record_count"`
}

type ManifestSender struct {
	OrganizationID string `json:"organization_id"`
	SignerID       string `json:"signer_id"`
	CertificateID  string `json:"certificate_id"`
}

type ManifestRecipient struct {
	ID            string `json:"id"`
	CertificateID string `json:"certificate_id"`
	WrappedKey    []byte `json:"wrapped_key"`
}

type ManifestCipher struct {
	Provider         string `json:"provider"`
	Algorithm        string `json:"algorithm"`
	Compression      string `json:"compression"`
	PayloadSHA256    string `json:"payload_sha256"`
	CiphertextSHA256 string `json:"ciphertext_sha256"`
	PlaintextSize    int    `json:"plaintext_size"`
}

type signatureDocument struct {
	Provider      string `json:"provider"`
	Algorithm     string `json:"algorithm"`
	SignerID      string `json:"signer_id"`
	CertificateID string `json:"certificate_id"`
	Value         []byte `json:"value"`
}

// container — разобранный, но ещё не проверенный пакет.
type container struct {
	manifestBytes []byte
	signature     cryptoengine.Signature
	ciphertext    []byte
}

// assemble собирает файл пакета.
func assemble(manifest []byte, signature cryptoengine.Signature, ciphertext []byte) ([]byte, error) {
	document, err := json.Marshal(signatureDocument{
		Provider: signature.Provider, Algorithm: signature.Algorithm,
		SignerID: signature.SignerID, CertificateID: signature.CertificateID, Value: signature.Value,
	})
	if err != nil {
		return nil, err
	}
	total := len(Magic) + 1 + 4 + len(manifest) + 4 + len(document) + 8 + len(ciphertext)
	if len(manifest) > MaxManifestBytes || len(document) > MaxSignatureBytes || total > MaxPackageBytes {
		return nil, ErrPackageTooLarge
	}
	var out bytes.Buffer
	out.Grow(total)
	out.WriteString(Magic)
	out.WriteByte(FormatVersion)
	writeUint32(&out, uint32(len(manifest)))
	out.Write(manifest)
	writeUint32(&out, uint32(len(document)))
	out.Write(document)
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(ciphertext)))
	out.Write(size[:])
	out.Write(ciphertext)
	return out.Bytes(), nil
}

func writeUint32(out *bytes.Buffer, value uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], value)
	out.Write(buf[:])
}

// parseContainer разбирает файл строго: заявленные длины проверяются до
// выделения памяти, лишние байты в конце — ошибка, а не «игнорируем».
func parseContainer(data []byte) (container, error) {
	if len(data) > MaxPackageBytes {
		return container{}, ErrPackageTooLarge
	}
	reader := bytes.NewReader(data)
	header := make([]byte, len(Magic)+1)
	if _, err := reader.Read(header); err != nil || string(header[:len(Magic)]) != Magic {
		return container{}, ErrPackageFormat
	}
	if header[len(Magic)] != FormatVersion {
		return container{}, ErrUnsupportedVersion
	}
	manifest, err := readBlock32(reader, MaxManifestBytes)
	if err != nil {
		return container{}, err
	}
	signatureBytes, err := readBlock32(reader, MaxSignatureBytes)
	if err != nil {
		return container{}, err
	}
	var size [8]byte
	if n, _ := reader.Read(size[:]); n != len(size) {
		return container{}, ErrPackageFormat
	}
	ciphertextLen := binary.BigEndian.Uint64(size[:])
	if ciphertextLen == 0 || ciphertextLen != uint64(reader.Len()) {
		return container{}, ErrPackageFormat
	}
	ciphertext := make([]byte, ciphertextLen)
	if _, err := reader.Read(ciphertext); err != nil {
		return container{}, ErrPackageFormat
	}

	var document signatureDocument
	decoder := json.NewDecoder(bytes.NewReader(signatureBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return container{}, ErrPackageFormat
	}
	return container{
		manifestBytes: manifest,
		signature: cryptoengine.Signature{
			Provider: document.Provider, Algorithm: document.Algorithm,
			SignerID: document.SignerID, CertificateID: document.CertificateID, Value: document.Value,
		},
		ciphertext: ciphertext,
	}, nil
}

func readBlock32(reader *bytes.Reader, limit int) ([]byte, error) {
	var size [4]byte
	if n, _ := reader.Read(size[:]); n != len(size) {
		return nil, ErrPackageFormat
	}
	length := binary.BigEndian.Uint32(size[:])
	if int64(length) > int64(limit) {
		return nil, ErrPackageTooLarge
	}
	if int64(length) > int64(reader.Len()) || length == 0 {
		return nil, ErrPackageFormat
	}
	block := make([]byte, length)
	if _, err := reader.Read(block); err != nil {
		return nil, ErrPackageFormat
	}
	return block, nil
}

// parseManifest разбирает манифест и требует, чтобы сохранённые байты были
// канонической записью: подпись покрывает именно их, и «почти такой же» JSON с
// другим порядком ключей — уже другой документ.
func parseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: манифест: %v", ErrPackageFormat, err)
	}
	if decoder.More() {
		return Manifest{}, fmt.Errorf("%w: лишние данные после манифеста", ErrPackageFormat)
	}
	canonical, err := canonicalManifest(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if !bytes.Equal(canonical, raw) {
		return Manifest{}, fmt.Errorf("%w: манифест записан не в канонической форме", ErrPackageFormat)
	}
	return manifest, nil
}

func canonicalManifest(manifest Manifest) ([]byte, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	return cryptoengine.CanonicalJSON(encoded)
}
