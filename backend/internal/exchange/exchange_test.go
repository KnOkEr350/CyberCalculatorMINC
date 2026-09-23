package exchange

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"cybercalc/internal/cryptoengine"
)

// CRYPTO-04 + CRYPTO-05: экспорт и импорт — полный круг. Получатель видит те
// же записи, манифест описывает пакет, а подписант установлен по сертификату.
func TestExportThenImportRoundTrip(t *testing.T) {
	f := newFixture(t)
	data := f.export(t, sampleRecords())

	imported, err := f.open(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Records) != 3 {
		t.Fatalf("получено %d записей, ожидалось 3", len(imported.Records))
	}
	manifest := imported.Manifest
	if manifest.Format != FormatName || manifest.Version != FormatVersion || manifest.Schema != SchemaRecords {
		t.Fatalf("манифест: %+v", manifest)
	}
	if manifest.ReportYear != 2026 || manifest.Period != "plan" || manifest.RecordCount != 3 {
		t.Fatalf("параметры пакета в манифесте: %+v", manifest)
	}
	if !manifest.CreatedAt.Equal(fixtureNow) {
		t.Fatalf("время создания %s", manifest.CreatedAt)
	}
	if imported.Signer.OrganizationID != "org-a" || imported.Signer.SignerID != "signer-a" {
		t.Fatalf("подписант установлен неверно: %+v", imported.Signer)
	}
	if len(manifest.Recipients) != 1 || manifest.Recipients[0].ID != "org-b" || len(manifest.Recipients[0].WrappedKey) == 0 {
		t.Fatalf("получатели в манифесте: %+v", manifest.Recipients)
	}

	// Содержимое совпадает: сравнение по смыслу, как это делает сверка данных.
	result, err := Diff(sampleRecords(), imported.Records)
	if err != nil {
		t.Fatal(err)
	}
	if result.Counts[StatusIdentical] != 3 || len(result.Items) != 3 {
		t.Fatalf("после круга записи должны совпасть полностью: %v", result.Counts)
	}
}

// Один и тот же набор записей независимо от порядка даёт одно содержимое, а
// пакет для нескольких получателей открывает каждый из них.
func TestExportIsOrderIndependentAndMultiRecipient(t *testing.T) {
	f := newFixture(t)
	forward := sampleRecords()
	backward := []Record{forward[2], forward[1], forward[0]}

	var plaintexts [][]byte
	request := f.request(forward)
	request.Recipients = []cryptoengine.RecipientRef{f.recipient, f.stranger}
	if _, err := Export(context.Background(), f.engine, request); err != nil {
		t.Fatal(err)
	}
	request.Records = backward
	if _, err := Export(context.Background(), f.engine, request); err != nil {
		t.Fatal(err)
	}
	plaintexts = f.engine.encrypted
	if len(plaintexts) != 2 || !bytes.Equal(plaintexts[0], plaintexts[1]) {
		t.Fatal("порядок записей не должен менять содержимое пакета")
	}

	data, err := Export(context.Background(), f.engine, request)
	if err != nil {
		t.Fatal(err)
	}
	f.policy.Recipient = cryptoengine.RecipientSecretRef{ID: "org-c", KeyRef: "key-stranger"}
	if imported, err := f.open(data); err != nil || len(imported.Records) != 3 {
		t.Fatalf("второй получатель должен открыть пакет: %v", err)
	}
}

// «Plaintext не пишется на диск»: экспорт работает целиком в памяти, а на
// шифрование уходит уже сжатое содержимое; на подпись — только манифест.
func TestExportKeepsPlaintextOutOfEverythingButEncryption(t *testing.T) {
	f := newFixture(t)
	data := f.export(t, sampleRecords())

	for _, marker := range []string{"Иванов", "Разработка ПО", "Сидорова", "teacher_full_name", "specialty_code"} {
		if bytes.Contains(data, []byte(marker)) {
			t.Errorf("в пакете виден открытый текст %q", marker)
		}
	}
	if len(f.engine.encrypted) != 1 {
		t.Fatalf("шифрование вызвано %d раз", len(f.engine.encrypted))
	}
	if bytes.Contains(f.engine.encrypted[0], []byte("Иванов")) {
		t.Error("на шифрование ушёл несжатый текст: содержимое должно быть сжато до шифрования")
	}
	for _, signed := range f.engine.signed {
		if bytes.Contains(signed, []byte("Иванов")) || bytes.Contains(signed, []byte("Разработка ПО")) {
			t.Error("подписывается содержимое, а не манифест")
		}
	}
	// Манифест не раскрывает содержимого: только счётчики и хеши.
	parsed, err := parseContainer(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(parsed.manifestBytes), "teachers") || strings.Contains(string(parsed.manifestBytes), "Иванов") {
		t.Error("манифест раскрывает содержимое пакета")
	}
}

func TestExportRejectsInvalidRequests(t *testing.T) {
	f := newFixture(t)
	cases := map[string]func(*ExportRequest){
		"нет организации":      func(r *ExportRequest) { r.OrganizationID = "" },
		"нет ссылки на ключ":   func(r *ExportRequest) { r.Signer.KeyRef = "" },
		"нет сертификата":      func(r *ExportRequest) { r.Signer.CertificateID = "" },
		"нет получателей":      func(r *ExportRequest) { r.Recipients = nil },
		"получатель без ключа": func(r *ExportRequest) { r.Recipients = []cryptoengine.RecipientRef{{ID: "x", CertificateID: "c"}} },
		"получатель без серт.": func(r *ExportRequest) { r.Recipients[0].CertificateID = "" },
		"повтор получателя":    func(r *ExportRequest) { r.Recipients = append(r.Recipients, r.Recipients[0]) },
		"год вне диапазона":    func(r *ExportRequest) { r.ReportYear = 1999 },
		"неизвестный период":   func(r *ExportRequest) { r.Period = "year" },
		"нет записей":          func(r *ExportRequest) { r.Records = nil },
		"вид вне обмена":       func(r *ExportRequest) { r.Records = []Record{{CategoryCode: "нет-такого"}} },
		"запись без ключа": func(r *ExportRequest) {
			r.Records = []Record{{CategoryCode: "teachers", Payload: map[string]interface{}{}}}
		},
		"дубликат составного ключа": func(r *ExportRequest) { r.Records = append(r.Records, r.Records[0]) },
	}
	for name, mutate := range cases {
		request := f.request(sampleRecords())
		mutate(&request)
		if _, err := Export(context.Background(), f.engine, request); err == nil {
			t.Errorf("%s: запрос должен отклоняться", name)
		}
	}
	// Ссылка на неизвестный ключ подписи: подписать нечем.
	request := f.request(sampleRecords())
	request.Signer.KeyRef = "нет-такого"
	if _, err := Export(context.Background(), f.engine, request); !errors.Is(err, cryptoengine.ErrInvalidSignature) {
		t.Errorf("неизвестный ключ подписи: %v", err)
	}
}

// CRYPTO-03: формат разбирается строго. Каждый вид повреждения даёт понятную
// ошибку, а не панику и не разбор «того, что получилось».
func TestContainerParsingIsStrict(t *testing.T) {
	f := newFixture(t)
	good := f.export(t, sampleRecords())
	if _, err := parseContainer(good); err != nil {
		t.Fatal(err)
	}

	withByte := func(offset int, value byte) []byte {
		out := append([]byte{}, good...)
		out[offset] = value
		return out
	}
	manifestLen := int(binary.BigEndian.Uint32(good[len(Magic)+1:]))
	manifestEnd := len(Magic) + 1 + 4 + manifestLen

	cases := map[string]struct {
		data []byte
		want error
	}{
		"пустой файл":                  {nil, ErrPackageFormat},
		"не тот заголовок":             {[]byte("NOTAPACKAGE-------------"), ErrPackageFormat},
		"другая версия формата":        {withByte(len(Magic), 2), ErrUnsupportedVersion},
		"обрыв в заголовке":            {good[:len(Magic)+1], ErrPackageFormat},
		"обрыв в манифесте":            {good[:len(Magic)+1+4+10], ErrPackageFormat},
		"обрыв в подписи":              {good[:manifestEnd+6], ErrPackageFormat},
		"обрыв в шифртексте":           {good[:len(good)-5], ErrPackageFormat},
		"лишние байты в конце":         {append(append([]byte{}, good...), 0, 0, 0), ErrPackageFormat},
		"нулевая длина манифеста":      {append(append([]byte(Magic), FormatVersion), 0, 0, 0, 0), ErrPackageFormat},
		"манифест больше предела":      {append(append([]byte(Magic), FormatVersion), 0xff, 0xff, 0xff, 0xff), ErrPackageTooLarge},
		"длина шифртекста не сходится": {withByte(manifestEnd+4+int(binary.BigEndian.Uint32(good[manifestEnd:])), 0x7f), ErrPackageFormat},
	}
	for name, tc := range cases {
		if _, err := parseContainer(tc.data); !errors.Is(err, tc.want) {
			t.Errorf("%s: получена ошибка %v, ожидалась %v", name, err, tc.want)
		}
	}
	// Заявленные длины проверяются до выделения памяти: файл в четыре байта не
	// может заставить разбор выделить гигабайты.
	if _, err := parseContainer(append(append([]byte(Magic), FormatVersion), 0x7f, 0xff, 0xff, 0xff)); err == nil {
		t.Error("огромная заявленная длина должна отклоняться")
	}
	// Реальный файл больше предела.
	if _, err := parseContainer(make([]byte, MaxPackageBytes+1)); !errors.Is(err, ErrPackageTooLarge) {
		t.Errorf("файл больше предела: %v", err)
	}
}

// Манифест хранится в канонической форме: подпись покрывает именно эти байты, а
// эквивалентный по смыслу JSON с другим порядком ключей — другой документ.
func TestManifestMustBeCanonical(t *testing.T) {
	f := newFixture(t)
	parsed, err := parseContainer(f.export(t, sampleRecords()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseManifest(parsed.manifestBytes); err != nil {
		t.Fatal(err)
	}
	// Тот же JSON с пробелами.
	spaced := bytes.ReplaceAll(parsed.manifestBytes, []byte(`","`), []byte(`", "`))
	if _, err := parseManifest(spaced); !errors.Is(err, ErrPackageFormat) {
		t.Errorf("манифест с лишними пробелами: %v", err)
	}
	// Неизвестное поле.
	extra := bytes.Replace(parsed.manifestBytes, []byte(`{"cipher"`), []byte(`{"backdoor":1,"cipher"`), 1)
	if _, err := parseManifest(extra); !errors.Is(err, ErrPackageFormat) {
		t.Errorf("манифест с неизвестным полем: %v", err)
	}
	// Данные после документа.
	if _, err := parseManifest(append(append([]byte{}, parsed.manifestBytes...), []byte(`{}`)...)); !errors.Is(err, ErrPackageFormat) {
		t.Errorf("данные после манифеста: %v", err)
	}
}

// QA-06: получатель. Пакет открывает только тот, кому он адресован, и только
// своим ключом.
func TestOnlyTheAddresseeOpensThePackage(t *testing.T) {
	f := newFixture(t)
	data := f.export(t, sampleRecords())

	stranger := f.policy
	stranger.Recipient = cryptoengine.RecipientSecretRef{ID: "org-c", KeyRef: "key-stranger"}
	if _, err := Open(context.Background(), f.engine, stranger, data); !errors.Is(err, ErrNotARecipient) {
		t.Fatalf("получатель, которого нет в манифесте: %v", err)
	}
	wrongKey := f.policy
	wrongKey.Recipient = cryptoengine.RecipientSecretRef{ID: "org-b", KeyRef: "key-stranger"}
	if _, err := Open(context.Background(), f.engine, wrongKey, data); !errors.Is(err, cryptoengine.ErrInvalidRecipient) {
		t.Fatalf("чужой закрытый ключ под именем адресата: %v", err)
	}
}

// QA-06: подписант. Просроченный, ещё не действующий, отозванный, с чужим
// корнем и без права проверки — ни один не проходит, и до расшифровки дело не
// доходит.
func TestSignerCertificateIsValidatedBeforeDecryption(t *testing.T) {
	data := func(f *fixture) []byte { return f.export(t, sampleRecords()) }

	cases := map[string]struct {
		mutate func(*fixture)
		want   error
	}{
		"сертификат просрочен": {func(f *fixture) {
			f.policy.Signature.Now = time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)
		}, cryptoengine.ErrCertificateExpired},
		"сертификат ещё не действует": {func(f *fixture) {
			f.policy.Signature.Now = time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)
		}, cryptoengine.ErrCertificateExpired},
		"сертификат отозван списком": {func(f *fixture) {
			f.policy.Signature.RevokedCertificateIDs = map[string]bool{"signer-a": true}
		}, cryptoengine.ErrCertificateRevoked},
		"корневой сертификат отозван": {func(f *fixture) {
			f.policy.Signature.RevokedCertificateIDs = map[string]bool{"root-a": true}
		}, cryptoengine.ErrCertificateRevoked},
		"сертификат помечен отозванным": {func(f *fixture) {
			revokedAt := fixtureNow.Add(-time.Hour)
			f.leaf.Status, f.leaf.RevokedAt = cryptoengine.CertificateRevoked, &revokedAt
			f.chains["cert-signer-a"] = []cryptoengine.CertificateRef{f.leaf, f.root}
		}, cryptoengine.ErrCertificateRevoked},
		"сертификат приостановлен": {func(f *fixture) {
			f.leaf.Status = cryptoengine.CertificateSuspended
			f.chains["cert-signer-a"] = []cryptoengine.CertificateRef{f.leaf, f.root}
		}, cryptoengine.ErrCertificateInvalid},
		"недоверенный корень": {func(f *fixture) {
			f.policy.Signature.TrustedRootIDs = map[string]bool{"другой-корень": true}
		}, cryptoengine.ErrUntrustedChain},
		"разорванная цепочка": {func(f *fixture) {
			f.leaf.IssuerID = "неизвестный-издатель"
			f.chains["cert-signer-a"] = []cryptoengine.CertificateRef{f.leaf, f.root}
		}, cryptoengine.ErrUntrustedChain},
		"нет цепочки": {func(f *fixture) {
			delete(f.chains, "cert-signer-a")
		}, cryptoengine.ErrUntrustedChain},
		"нет права проверки": {func(f *fixture) {
			f.leaf.Usages = []cryptoengine.KeyUsage{cryptoengine.KeyUsageSign}
			f.chains["cert-signer-a"] = []cryptoengine.CertificateRef{f.leaf, f.root}
		}, cryptoengine.ErrKeyUsageDenied},
	}
	for name, tc := range cases {
		f := newFixture(t)
		pkg := data(f)
		tc.mutate(f)
		f.engine.decrypts = 0
		if _, err := f.open(pkg); !errors.Is(err, tc.want) {
			t.Errorf("%s: получена ошибка %v, ожидалась %v", name, err, tc.want)
		}
		if f.engine.decrypts != 0 {
			t.Errorf("%s: расшифровка запущена до проверки подписанта", name)
		}
	}
}

// QA-06: подмена. Любое изменение любой части пакета обнаруживается до
// расшифровки.
func TestTamperingIsDetectedBeforeDecryption(t *testing.T) {
	f := newFixture(t)
	good := f.export(t, sampleRecords())
	parsed, err := parseContainer(good)
	if err != nil {
		t.Fatal(err)
	}
	manifestStart := len(Magic) + 1 + 4
	signatureStart := manifestStart + len(parsed.manifestBytes) + 4
	ciphertextStart := len(good) - len(parsed.ciphertext)

	flip := func(offset int) []byte {
		out := append([]byte{}, good...)
		out[offset] ^= 1
		return out
	}
	cases := map[string][]byte{
		"бит в шифртексте (начало)": flip(ciphertextStart),
		"бит в шифртексте (конец)":  flip(len(good) - 1),
		"бит в манифесте":           flip(manifestStart + len(parsed.manifestBytes)/2),
		"бит в подписи":             flip(signatureStart + 5),
	}
	for name, data := range cases {
		f.engine.decrypts = 0
		if _, err := f.open(data); err == nil {
			t.Errorf("%s: изменённый пакет открылся", name)
		}
		if f.engine.decrypts != 0 {
			t.Errorf("%s: расшифровка запущена до обнаружения подмены", name)
		}
	}

	// Манифест изменён «правильно» (канонически), но не переподписан.
	parsedManifest, err := parseManifest(parsed.manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	parsedManifest.RecordCount++
	forged, err := canonicalManifest(parsedManifest)
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := assemble(forged, parsed.signature, parsed.ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	f.engine.decrypts = 0
	if _, err := f.open(unsigned); !errors.Is(err, cryptoengine.ErrInvalidSignature) {
		t.Errorf("каноничная подмена манифеста без переподписи: %v", err)
	}
	if f.engine.decrypts != 0 {
		t.Error("расшифровка запущена при неверной подписи")
	}

	// Шифртекст заменён, а хеш в манифесте оставлен прежним.
	other := f.export(t, []Record{teacherRecord("Иной Человек Иванович", "Иной курс", 8)})
	otherParsed, _ := parseContainer(other)
	swapped, err := assemble(parsed.manifestBytes, parsed.signature, otherParsed.ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(swapped); !errors.Is(err, ErrTampered) {
		t.Errorf("подмена шифртекста: %v", err)
	}
}

// QA-06: отправитель. Организация в манифесте должна быть владельцем
// сертификата подписи, а подпись — принадлежать заявленному подписанту.
func TestSenderIdentityIsBoundToTheCertificate(t *testing.T) {
	f := newFixture(t)

	request := f.request(sampleRecords())
	request.OrganizationID = "org-evil" // подписант org-a выдаёт пакет за чужой
	forged, err := Export(context.Background(), f.engine, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.open(forged); !errors.Is(err, ErrSenderMismatch) {
		t.Errorf("пакет от имени чужой организации: %v", err)
	}

	good := f.export(t, sampleRecords())
	for name, mutate := range map[string]func(*Manifest){
		"другой подписант":  func(m *Manifest) { m.Sender.SignerID = "signer-x" },
		"другой сертификат": func(m *Manifest) { m.Sender.CertificateID = "cert-x" },
	} {
		data := f.resign(t, good, mutate)
		if _, err := f.open(data); err == nil {
			t.Errorf("%s: пакет должен отклоняться", name)
		}
	}
}

// QA-06: понижение. Другой алгоритм, схема, версия, период или год — отказ, даже
// если пакет подписан настоящим ключом отправителя.
func TestDowngradeAndScopeChangesAreRejected(t *testing.T) {
	f := newFixture(t)
	good := f.export(t, sampleRecords())

	cases := map[string]struct {
		mutate func(*Manifest)
		want   error
	}{
		"слабый шифр":               {func(m *Manifest) { m.Cipher.Algorithm = "fixture-xor-sha256" }, ErrAlgorithmNotAllowed},
		"другой провайдер":          {func(m *Manifest) { m.Cipher.Provider = "fixture" }, ErrAlgorithmNotAllowed},
		"другое сжатие":             {func(m *Manifest) { m.Cipher.Compression = "none" }, ErrAlgorithmNotAllowed},
		"прежняя схема":             {func(m *Manifest) { m.Schema = "exchange.records.v0" }, ErrSchemaMismatch},
		"будущая версия":            {func(m *Manifest) { m.Version = 2 }, ErrUnsupportedVersion},
		"другое имя формата":        {func(m *Manifest) { m.Format = "other.pkg" }, ErrUnsupportedVersion},
		"другой год":                {func(m *Manifest) { m.ReportYear = 2025 }, ErrSchemaMismatch},
		"другой период":             {func(m *Manifest) { m.Period = "fact" }, ErrSchemaMismatch},
		"нет получателей":           {func(m *Manifest) { m.Recipients = nil }, ErrPackageFormat},
		"число записей вне нормы":   {func(m *Manifest) { m.RecordCount = MaxRecords + 1 }, ErrPackageFormat},
		"число записей не сходится": {func(m *Manifest) { m.RecordCount = 99 }, ErrRecordCountMismatch},
	}
	for name, tc := range cases {
		if _, err := f.open(f.resign(t, good, tc.mutate)); !errors.Is(err, tc.want) {
			t.Errorf("%s: получена ошибка %v, ожидалась %v", name, err, tc.want)
		}
	}

	// Слабая подпись: подпись другого провайдера не входит в разрешённый список.
	weak := f.policy
	weak.AllowedSignatures = map[string]bool{"fixture/fixture-sha256": true}
	if _, err := Open(context.Background(), f.engine, weak, good); !errors.Is(err, ErrAlgorithmNotAllowed) {
		t.Errorf("подпись вне списка: %v", err)
	}
	// Закрытые списки по умолчанию пусты: не разрешено ничего.
	closed := f.policy
	closed.AllowedCiphers = nil
	if _, err := Open(context.Background(), f.engine, closed, good); !errors.Is(err, ErrAlgorithmNotAllowed) {
		t.Errorf("пустой список шифров: %v", err)
	}
	// Без источника цепочек доверять нечему.
	blind := f.policy
	blind.Chains = nil
	if _, err := Open(context.Background(), f.engine, blind, good); err == nil {
		t.Error("без источника цепочек пакет не должен открываться")
	}
}

// QA-06: «бомба» и размеры. Сжатое содержимое, разворачивающееся за предел,
// останавливается на границе, а не в памяти.
func TestDecompressionBombIsStopped(t *testing.T) {
	f := newFixture(t)
	original := plaintextLimit
	defer func() { plaintextLimit = original }()

	// Запись с большим, но хорошо сжимаемым значением: пакет мал, а
	// распакованное содержимое велико.
	big := teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64)
	big.Payload["notes"] = strings.Repeat("0", 2<<20)
	data := f.export(t, []Record{big})
	if len(data) > 64<<10 {
		t.Fatalf("пакет-«бомба» вышел слишком большим (%d байт): проверка не показательна", len(data))
	}

	plaintextLimit = 512 << 10
	if _, err := f.open(data); !errors.Is(err, ErrDecompressionLimit) {
		t.Fatalf("распаковка сверх предела: %v", err)
	}
	// При достаточном пределе тот же пакет открывается.
	plaintextLimit = 8 << 20
	if _, err := f.open(data); err != nil {
		t.Fatalf("при достаточном пределе пакет должен открываться: %v", err)
	}
}

func TestOversizedPackageIsRejectedOnExport(t *testing.T) {
	f := newFixture(t)
	records := []Record{}
	for i := 0; i < 3; i++ {
		record := teacherRecord("Иванов Иван Иванович", "Курс "+strings.Repeat("я", i+1), 64)
		records = append(records, record)
	}
	request := f.request(records)
	request.Records = append(request.Records, make([]Record, MaxRecords)...)
	if _, err := Export(context.Background(), f.engine, request); err == nil {
		t.Fatal("пакет с числом записей больше предела должен отклоняться при экспорте")
	}
}

// Отмена операции прерывает и экспорт, и импорт: зависший провайдер не держит
// запрос.
func TestCancellationStopsExportAndImport(t *testing.T) {
	f := newFixture(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Export(cancelled, f.engine, f.request(sampleRecords())); err == nil {
		t.Error("экспорт при отменённом контексте должен прерываться")
	}
	good := f.export(t, sampleRecords())
	if _, err := Open(cancelled, f.engine, f.policy, good); err == nil {
		t.Error("импорт при отменённом контексте должен прерываться")
	}
}
