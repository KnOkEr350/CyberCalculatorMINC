package exchange

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cybercalc/internal/cryptoengine"
)

var fixtureNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

type chains map[string][]cryptoengine.CertificateRef

func (c chains) Chain(_ context.Context, certificateID string) ([]cryptoengine.CertificateRef, error) {
	chain, ok := c[certificateID]
	if !ok {
		return nil, cryptoengine.ErrUntrustedChain
	}
	return chain, nil
}

// spyEngine считает вызовы: по ним проверяется порядок проверок и то, что
// именно попадает на подпись и шифрование.
type spyEngine struct {
	cryptoengine.Engine
	encrypted [][]byte
	signed    [][]byte
	decrypts  int
}

func (s *spyEngine) EncryptForRecipients(ctx context.Context, plaintext []byte, recipients []cryptoengine.RecipientRef) (cryptoengine.Package, error) {
	s.encrypted = append(s.encrypted, append([]byte{}, plaintext...))
	return s.Engine.EncryptForRecipients(ctx, plaintext, recipients)
}

func (s *spyEngine) Sign(ctx context.Context, data []byte, signer cryptoengine.SignerRef) (cryptoengine.Signature, error) {
	s.signed = append(s.signed, append([]byte{}, data...))
	return s.Engine.Sign(ctx, data, signer)
}

func (s *spyEngine) Decrypt(ctx context.Context, pkg cryptoengine.Package, recipient cryptoengine.RecipientSecretRef) ([]byte, error) {
	s.decrypts++
	return s.Engine.Decrypt(ctx, pkg, recipient)
}

type fixture struct {
	engine    *spyEngine
	ring      *cryptoengine.MemoryKeyring
	signer    cryptoengine.SignerRef
	recipient cryptoengine.RecipientRef
	stranger  cryptoengine.RecipientRef
	root      cryptoengine.CertificateRef
	leaf      cryptoengine.CertificateRef
	chains    chains
	policy    ImportPolicy
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ring := cryptoengine.NewMemoryKeyring()
	receiverPublic, err := ring.NewRecipient("key-receiver")
	if err != nil {
		t.Fatal(err)
	}
	strangerPublic, err := ring.NewRecipient("key-stranger")
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.NewSigner("sign-sender", "cert-signer-a"); err != nil {
		t.Fatal(err)
	}
	certificate := func(id, issuer string, usages ...cryptoengine.KeyUsage) cryptoengine.CertificateRef {
		return cryptoengine.CertificateRef{
			ID: id, OrganizationID: "org-a", PublicKeyID: "pub-" + id, CertificateID: "cert-" + id, IssuerID: issuer,
			Status:    cryptoengine.CertificateActive,
			ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ValidUntil: time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			Usages: usages,
		}
	}
	root := certificate("root-a", "", cryptoengine.KeyUsageVerify)
	leaf := certificate("signer-a", "root-a", cryptoengine.KeyUsageVerify, cryptoengine.KeyUsageSign)
	f := &fixture{
		engine:    &spyEngine{Engine: cryptoengine.NewSoftware(ring, ring)},
		ring:      ring,
		signer:    cryptoengine.SignerRef{ID: "signer-a", CertificateID: "cert-signer-a", KeyRef: "sign-sender"},
		recipient: cryptoengine.RecipientRef{ID: "org-b", CertificateID: "cert-b", PublicKeyBytes: receiverPublic},
		stranger:  cryptoengine.RecipientRef{ID: "org-c", CertificateID: "cert-c", PublicKeyBytes: strangerPublic},
		root:      root, leaf: leaf,
		chains: chains{"cert-signer-a": {leaf, root}},
	}
	f.policy = ImportPolicy{
		Recipient: cryptoengine.RecipientSecretRef{ID: "org-b", KeyRef: "key-receiver"},
		Signature: cryptoengine.SignaturePolicy{Now: fixtureNow, TrustedRootIDs: map[string]bool{"root-a": true},
			RequiredUsage: cryptoengine.KeyUsageVerify},
		Chains: f.chains,
		AllowedCiphers: map[string]bool{
			CipherKey(cryptoengine.SoftwareProviderName, cryptoengine.SoftwareCipherAlgorithm): true},
		AllowedSignatures: map[string]bool{
			CipherKey(cryptoengine.SoftwareProviderName, cryptoengine.SoftwareSignatureAlgorithm): true},
		ExpectedYear: 2026, ExpectedPeriod: "plan",
	}
	return f
}

func (f *fixture) request(records []Record) ExportRequest {
	return ExportRequest{
		OrganizationID: "org-a", Signer: f.signer, Recipients: []cryptoengine.RecipientRef{f.recipient},
		Records: records, ReportYear: 2026, Period: "plan", Now: fixtureNow,
	}
}

func (f *fixture) export(t *testing.T, records []Record) []byte {
	t.Helper()
	data, err := Export(context.Background(), f.engine, f.request(records))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f *fixture) open(data []byte) (Imported, error) {
	return Open(context.Background(), f.engine, f.policy, data)
}

// resign переписывает манифест уже собранного пакета и подписывает его заново
// настоящим ключом отправителя. Так проверяются отказы по политике: пакет
// формально безупречен, и единственная причина отказа — то, что изменено.
func (f *fixture) resign(t *testing.T, data []byte, mutate func(*Manifest)) []byte {
	t.Helper()
	parsed, err := parseContainer(data)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := parseManifest(parsed.manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&manifest)
	canonical, err := canonicalManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := f.engine.Engine.Sign(context.Background(), canonical, f.signer)
	if err != nil {
		t.Fatal(err)
	}
	out, err := assemble(canonical, signature, parsed.ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func teacherRecord(name, course string, hours int) Record {
	return Record{
		CategoryCode: "teachers", Audience: "vuz", AmountRub: fmt.Sprintf("%d.00", hours*4140),
		Payload: map[string]interface{}{
			"teacher_full_name": name, "course_name": course, "semester": 3, "academic_hours": hours,
			"specialty_code": "09.03.01", "org_name": "uuid-локального-партнёра", "staff_member_id": "uuid-локального-сотрудника",
		},
	}
}

func internshipRecord(student string) Record {
	return Record{
		CategoryCode: "internship", Audience: "vuz", AmountRub: "204000.00",
		Payload: map[string]interface{}{
			"student_full_name": student, "course": "3", "period_start": "2026-02-01", "period_end": "2026-04-30",
			"specialty_code": "09.03.02", "duration_months": 3,
		},
	}
}

func sampleRecords() []Record {
	return []Record{
		teacherRecord("Иванов Иван Иванович", "Разработка ПО", 64),
		teacherRecord("Петров Пётр Петрович", "Базы данных", 32),
		internshipRecord("Сидорова Анна Сергеевна"),
	}
}
