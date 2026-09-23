package cryptoengine

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type softwareFixture struct {
	engine *Software
	ring   *MemoryKeyring
	alice  RecipientRef
	bob    RecipientRef
}

func newSoftwareFixture(t *testing.T) softwareFixture {
	t.Helper()
	ring := NewMemoryKeyring()
	alicePublic, err := ring.NewRecipient("key-alice")
	if err != nil {
		t.Fatal(err)
	}
	bobPublic, err := ring.NewRecipient("key-bob")
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.NewSigner("sign-alice", "cert-alice"); err != nil {
		t.Fatal(err)
	}
	return softwareFixture{
		engine: NewSoftware(ring, ring), ring: ring,
		alice: RecipientRef{ID: "alice", CertificateID: "cert-alice", PublicKeyBytes: alicePublic},
		bob:   RecipientRef{ID: "bob", CertificateID: "cert-bob", PublicKeyBytes: bobPublic},
	}
}

// Каждый адресат открывает пакет собственным ключом, и открытый текст
// возвращается без потерь.
func TestSoftwareEncryptsForEveryRecipient(t *testing.T) {
	f := newSoftwareFixture(t)
	ctx := context.Background()
	secret := []byte("плановые затраты по соглашению № 7")

	pkg, err := f.engine.EncryptForRecipients(ctx, secret, []RecipientRef{f.alice, f.bob})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(pkg.Ciphertext, secret) {
		t.Fatal("открытый текст виден в шифртексте")
	}
	for _, who := range []struct{ id, key string }{{"alice", "key-alice"}, {"bob", "key-bob"}} {
		got, err := f.engine.Decrypt(ctx, pkg, RecipientSecretRef{ID: who.id, KeyRef: who.key})
		if err != nil {
			t.Fatalf("%s: %v", who.id, err)
		}
		if !bytes.Equal(got, secret) {
			t.Fatalf("%s получил %q", who.id, got)
		}
	}
}

// Два шифрования одного текста дают разные шифртексты: ключ содержимого и
// nonce случайны, поэтому повтор не выдаёт, что данные те же.
func TestSoftwareEncryptionIsRandomised(t *testing.T) {
	f := newSoftwareFixture(t)
	first, _ := f.engine.EncryptForRecipients(context.Background(), []byte("один и тот же текст"), []RecipientRef{f.alice})
	second, _ := f.engine.EncryptForRecipients(context.Background(), []byte("один и тот же текст"), []RecipientRef{f.alice})
	if bytes.Equal(first.Ciphertext, second.Ciphertext) {
		t.Fatal("шифртексты одинаковы: nonce или ключ не случайны")
	}
	if bytes.Equal(first.Recipients[0].WrappedKey, second.Recipients[0].WrappedKey) {
		t.Fatal("обёрнутые ключи одинаковы: эфемерный ключ не случаен")
	}
}

func TestSoftwareRejectsWrongRecipientAndKey(t *testing.T) {
	f := newSoftwareFixture(t)
	ctx := context.Background()
	pkg, err := f.engine.EncryptForRecipients(ctx, []byte("секрет"), []RecipientRef{f.alice})
	if err != nil {
		t.Fatal(err)
	}
	// Получатель, которого в пакете нет.
	if _, err := f.engine.Decrypt(ctx, pkg, RecipientSecretRef{ID: "bob", KeyRef: "key-bob"}); !errors.Is(err, ErrInvalidRecipient) {
		t.Fatalf("посторонний получатель: %v", err)
	}
	// Чужой ключ под именем адресата: конверт не открывается.
	if _, err := f.engine.Decrypt(ctx, pkg, RecipientSecretRef{ID: "alice", KeyRef: "key-bob"}); !errors.Is(err, ErrInvalidRecipient) {
		t.Fatalf("чужой закрытый ключ: %v", err)
	}
	// Неизвестная ссылка на ключ.
	if _, err := f.engine.Decrypt(ctx, pkg, RecipientSecretRef{ID: "alice", KeyRef: "нет-такого"}); !errors.Is(err, ErrInvalidRecipient) {
		t.Fatalf("неизвестный ключ: %v", err)
	}
}

// Идентификатор получателя входит в защищаемые данные: конверт Алисы нельзя
// выдать за конверт Боба, даже если у злоумышленника есть закрытый ключ Боба.
func TestSoftwareEnvelopeIsBoundToItsRecipient(t *testing.T) {
	f := newSoftwareFixture(t)
	ctx := context.Background()
	pkg, err := f.engine.EncryptForRecipients(ctx, []byte("секрет"), []RecipientRef{f.alice, f.bob})
	if err != nil {
		t.Fatal(err)
	}
	swapped := pkg
	swapped.Recipients = []RecipientEnvelope{
		{RecipientID: "bob", WrappedKey: pkg.Recipients[0].WrappedKey}, // конверт Алисы под именем Боба
	}
	if _, err := f.engine.Decrypt(ctx, swapped, RecipientSecretRef{ID: "bob", KeyRef: "key-bob"}); err == nil {
		t.Fatal("переложенный конверт не должен открываться")
	}
}

// Любое изменение шифртекста, хеша, версии или алгоритма отвергается.
func TestSoftwareDetectsTamperingAndDowngrade(t *testing.T) {
	f := newSoftwareFixture(t)
	ctx := context.Background()
	recipient := RecipientSecretRef{ID: "alice", KeyRef: "key-alice"}
	fresh := func() Package {
		pkg, err := f.engine.EncryptForRecipients(ctx, []byte("секретный текст пакета"), []RecipientRef{f.alice})
		if err != nil {
			t.Fatal(err)
		}
		return pkg
	}
	mutations := map[string]func(*Package){
		"бит шифртекста":       func(p *Package) { p.Ciphertext[len(p.Ciphertext)-1] ^= 1 },
		"бит nonce":            func(p *Package) { p.Ciphertext[0] ^= 1 },
		"обрезанный текст":     func(p *Package) { p.Ciphertext = p.Ciphertext[:len(p.Ciphertext)-3] },
		"подменённый хеш":      func(p *Package) { p.PayloadHash[0] ^= 1 },
		"другой алгоритм":      func(p *Package) { p.Algorithm = "fixture-xor-sha256" },
		"другая версия":        func(p *Package) { p.Version = 2 },
		"другой провайдер":     func(p *Package) { p.Provider = FixtureProviderName },
		"пустой шифртекст":     func(p *Package) { p.Ciphertext = nil },
		"бит обёрнутого ключа": func(p *Package) { p.Recipients[0].WrappedKey[40] ^= 1 },
	}
	for name, mutate := range mutations {
		pkg := fresh()
		mutate(&pkg)
		if _, err := f.engine.Decrypt(ctx, pkg, recipient); err == nil {
			t.Errorf("%s: изменённый пакет открылся", name)
		}
	}
}

func TestSoftwareSignatureBindsSignerAndData(t *testing.T) {
	f := newSoftwareFixture(t)
	ctx := context.Background()
	signer := SignerRef{ID: "alice-signer", CertificateID: "cert-alice", KeyRef: "sign-alice"}
	data := []byte("подписываемый манифест")

	signature, err := f.engine.Sign(ctx, data, signer)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.engine.Verify(ctx, data, signature)
	if err != nil {
		t.Fatal(err)
	}
	if info.SignerID != "alice-signer" || info.CertificateID != "cert-alice" || info.Algorithm != SoftwareSignatureAlgorithm {
		t.Fatalf("сведения о подписанте: %+v", info)
	}

	// Изменённые данные.
	if _, err := f.engine.Verify(ctx, []byte("подписываемый манифест!"), signature); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("изменённые данные: %v", err)
	}
	// Изменённая подпись.
	broken := signature
	broken.Value = append([]byte{}, signature.Value...)
	broken.Value[0] ^= 1
	if _, err := f.engine.Verify(ctx, data, broken); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("изменённая подпись: %v", err)
	}
	// Подпись, выданная за сертификат другого владельца, проверяется его ключом.
	if err := f.ring.NewSigner("sign-mallory", "cert-mallory"); err != nil {
		t.Fatal(err)
	}
	forged, err := f.engine.Sign(ctx, data, SignerRef{ID: "alice-signer", CertificateID: "cert-mallory", KeyRef: "sign-mallory"})
	if err != nil {
		t.Fatal(err)
	}
	forged.CertificateID = "cert-alice"
	if _, err := f.engine.Verify(ctx, data, forged); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("подпись чужим ключом под чужим сертификатом: %v", err)
	}
	// Слабый или неизвестный алгоритм не принимается.
	weak := signature
	weak.Algorithm = "fixture-sha256"
	if _, err := f.engine.Verify(ctx, data, weak); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("чужой алгоритм: %v", err)
	}
	// Подписать чужим или несуществующим ключом нельзя.
	if _, err := f.engine.Sign(ctx, data, SignerRef{ID: "x", CertificateID: "c", KeyRef: "нет"}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("неизвестный ключ подписи: %v", err)
	}
}

func TestSoftwareRejectsInvalidRecipients(t *testing.T) {
	f := newSoftwareFixture(t)
	ctx := context.Background()
	for name, recipients := range map[string][]RecipientRef{
		"без получателей":       nil,
		"без идентификатора":    {{PublicKeyBytes: f.alice.PublicKeyBytes}},
		"повтор получателя":     {f.alice, f.alice},
		"короткий ключ":         {{ID: "x", PublicKeyBytes: []byte{1, 2, 3}}},
		"нулевой открытый ключ": {{ID: "x", PublicKeyBytes: make([]byte, 32)}},
	} {
		if _, err := f.engine.EncryptForRecipients(ctx, []byte("текст"), recipients); err == nil {
			t.Errorf("%s: должно отклоняться", name)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.engine.EncryptForRecipients(cancelled, []byte("текст"), []RecipientRef{f.alice}); err == nil {
		t.Error("отменённый контекст должен прерывать шифрование")
	}
}
