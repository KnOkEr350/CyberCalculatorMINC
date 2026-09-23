package tlsboot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func newCA(t *testing.T) (PEM, *x509.CertPool) {
	t.Helper()
	ca, err := NewCA("Локальный центр Киберкалькулятора", 5*365*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := Pool(ca.Cert)
	if err != nil {
		t.Fatal(err)
	}
	return ca, pool
}

func TestIssuedCertificateIsValidForItsHostsOnly(t *testing.T) {
	ca, roots := newCA(t)
	server, err := IssueServer(ca, []string{"calculator.internal", "10.20.30.40"}, 200*24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	info, err := Validate(server, "calculator.internal", roots, now)
	if err != nil {
		t.Fatal(err)
	}
	if info.Chain != 2 || len(info.Hosts) != 2 || info.Algorithm != "ECDSA-P-256" {
		t.Fatalf("сведения: %+v", info)
	}
	if _, err := Validate(server, "10.20.30.40", roots, now); err != nil {
		t.Fatalf("по адресу: %v", err)
	}
	if _, err := Validate(server, "other.internal", roots, now); err == nil {
		t.Fatal("чужое имя не должно проходить")
	}
	// Реальное TLS-рукопожатие с этой парой: клиент доверяет только центру.
	cert, err := tls.X509KeyPair(server.Cert, server.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.Certificate) != 2 {
		t.Fatalf("цепочка в файле: %d", len(cert.Certificate))
	}
}

func TestIssueRejectsUnsafeRequests(t *testing.T) {
	ca, _ := newCA(t)
	for name, call := range map[string]func() error{
		"без имён": func() error { _, err := IssueServer(ca, nil, time.Hour, now); return err },
		"слишком долгий": func() error {
			_, err := IssueServer(ca, []string{"a.internal"}, MaxServerValidity+time.Hour, now)
			return err
		},
		"нулевой срок": func() error { _, err := IssueServer(ca, []string{"a.internal"}, 0, now); return err },
		"дольше центра": func() error {
			_, err := IssueServer(ca, []string{"a.internal"}, MaxServerValidity, now.AddDate(6, 0, 0))
			return err
		},
		"имя с пробелом":  func() error { _, err := IssueServer(ca, []string{"a b"}, time.Hour, now); return err },
		"центр без имени": func() error { _, err := NewCA(" ", time.Hour, now); return err },
		"центр на 30 лет": func() error { _, err := NewCA("x", 30*365*24*time.Hour, now); return err },
		"не центр выпускает": func() error {
			leaf, _ := IssueServer(ca, []string{"a.internal"}, time.Hour, now)
			_, err := IssueServer(PEM{Cert: leaf.Cert, Key: leaf.Key}, []string{"b.internal"}, time.Hour, now)
			return err
		},
	} {
		if err := call(); err == nil {
			t.Errorf("%s: запрос должен отвергаться", name)
		}
	}
}

func TestValidateCatchesEveryDefect(t *testing.T) {
	ca, roots := newCA(t)
	good, _ := IssueServer(ca, []string{"calculator.internal"}, 30*24*time.Hour, now)
	other, _ := IssueServer(ca, []string{"calculator.internal"}, 30*24*time.Hour, now)

	if _, err := Validate(PEM{Cert: good.Cert, Key: other.Key}, "calculator.internal", roots, now); err == nil || !strings.Contains(err.Error(), "не подходит") {
		t.Fatalf("чужой ключ: %v", err)
	}
	if _, err := Validate(good, "calculator.internal", roots, now.AddDate(0, 0, 60)); err == nil || !strings.Contains(err.Error(), "истёк") {
		t.Fatalf("просроченный: %v", err)
	}
	if _, err := Validate(good, "calculator.internal", roots, now.AddDate(0, 0, -1)); err == nil || !strings.Contains(err.Error(), "ещё не действует") {
		t.Fatalf("ещё не действует: %v", err)
	}
	// Корень не тот: цепочка не строится.
	_, otherRoots := newCA(t)
	if _, err := Validate(good, "calculator.internal", otherRoots, now); err == nil || !strings.Contains(err.Error(), "цепочка") {
		t.Fatalf("недоверенный корень: %v", err)
	}
	if _, err := Validate(PEM{Cert: []byte("мусор"), Key: good.Key}, "", roots, now); err == nil {
		t.Fatal("мусор вместо сертификата")
	}
	if _, err := Validate(PEM{Cert: good.Cert, Key: []byte("мусор")}, "", roots, now); err == nil {
		t.Fatal("мусор вместо ключа")
	}
}

func TestWeakKeysAreRefused(t *testing.T) {
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "weak.internal"}, DNSNames: []string{"weak.internal"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &weak.PublicKey, weak)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(weak)})
	pair := PEM{Cert: encodeCert(der), Key: keyPEM}
	roots, _ := Pool(pair.Cert)
	if _, err := Validate(pair, "weak.internal", roots, now); err == nil || !strings.Contains(err.Error(), "2048") {
		t.Fatalf("RSA-1024 должен отвергаться: %v", err)
	}

	p224, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err = x509.CreateCertificate(rand.Reader, template, template, &p224.PublicKey, p224)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalPKCS8PrivateKey(p224)
	pair = PEM{Cert: encodeCert(der), Key: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})}
	if _, err := Validate(pair, "weak.internal", roots, now); err == nil || !strings.Contains(err.Error(), "P-256") {
		t.Fatalf("P-224 должен отвергаться: %v", err)
	}
}

func liveFile(t *testing.T, s Store, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(s.Dir, "live", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInstallSwitchesAtomicallyAndKeepsTheLiveOneOnFailure(t *testing.T) {
	ca, roots := newCA(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "tls")}
	first, _ := IssueServer(ca, []string{"calculator.internal"}, 30*24*time.Hour, now)
	if _, err := store.Install(first, "calculator.internal", roots, now); err != nil {
		t.Fatal(err)
	}
	if liveFile(t, store, "fullchain.pem") != string(first.Cert) {
		t.Fatal("действующая пара — первая")
	}
	info, err := os.Stat(filepath.Join(store.Dir, "live", "privkey.pem"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("ключ должен быть доступен только владельцу: %v %v", info, err)
	}

	// Непроверенная пара действующую не затрагивает.
	stranger, _ := IssueServer(ca, []string{"calculator.internal"}, 30*24*time.Hour, now)
	bad := PEM{Cert: stranger.Cert, Key: first.Key}
	if _, err := store.Install(bad, "calculator.internal", roots, now); err == nil {
		t.Fatal("пара с чужим ключом не устанавливается")
	}
	if liveFile(t, store, "fullchain.pem") != string(first.Cert) || len(store.list()) != 1 {
		t.Fatalf("после отказа действующая пара прежняя, лишних версий нет: %v", store.list())
	}

	second, _ := IssueServer(ca, []string{"calculator.internal"}, 60*24*time.Hour, now.Add(time.Hour))
	if _, err := store.Install(second, "calculator.internal", roots, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if liveFile(t, store, "fullchain.pem") != string(second.Cert) {
		t.Fatal("после замены действует вторая пара")
	}
}

func TestRollbackRestoresThePreviousPairAfterChecking(t *testing.T) {
	ca, roots := newCA(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "tls")}
	if _, err := store.Rollback("calculator.internal", roots, now); err == nil {
		t.Fatal("откатывать нечего")
	}
	first, _ := IssueServer(ca, []string{"calculator.internal"}, 30*24*time.Hour, now)
	second, _ := IssueServer(ca, []string{"calculator.internal"}, 60*24*time.Hour, now.Add(time.Hour))
	store.Install(first, "calculator.internal", roots, now)
	store.Install(second, "calculator.internal", roots, now.Add(time.Hour))
	if _, err := store.Rollback("calculator.internal", roots, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if liveFile(t, store, "fullchain.pem") != string(first.Cert) {
		t.Fatal("после отката действует первая пара")
	}
	// Первая пара к тому времени истекла: откат на неё отвергается.
	fresh := Store{Dir: filepath.Join(t.TempDir(), "tls2")}
	fresh.Install(first, "calculator.internal", roots, now)
	fresh.Install(second, "calculator.internal", roots, now.Add(time.Hour))
	if _, err := fresh.Rollback("calculator.internal", roots, now.AddDate(0, 0, 40)); err == nil || !strings.Contains(err.Error(), "непригодна") {
		t.Fatalf("истёкшая прежняя пара: %v", err)
	}
}

func TestOnlyRecentVersionsAreKept(t *testing.T) {
	ca, roots := newCA(t)
	store := Store{Dir: filepath.Join(t.TempDir(), "tls")}
	for i := 0; i < 6; i++ {
		at := now.Add(time.Duration(i) * time.Hour)
		pair, _ := IssueServer(ca, []string{"calculator.internal"}, 30*24*time.Hour, at)
		if _, err := store.Install(pair, "calculator.internal", roots, at); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(store.list()); got != KeepVersions {
		t.Fatalf("версий %d, ожидалось %d", got, KeepVersions)
	}
	if store.Live() == "" {
		t.Fatal("действующая версия сохраняется всегда")
	}
}

func TestNginxConfigRedirectsAndRefusesInjection(t *testing.T) {
	config, err := NginxConfig("calculator.internal", "/etc/cybercalc/tls", "127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"return 308 https://calculator.internal$request_uri", "listen 443 ssl", "ssl_protocols TLSv1.2 TLSv1.3",
		"Strict-Transport-Security", "/etc/cybercalc/tls/live/fullchain.pem", "proxy_pass http://127.0.0.1:8080", "server_tokens off"} {
		if !strings.Contains(config, want) {
			t.Errorf("в конфигурации нет %q", want)
		}
	}
	if strings.Contains(config, "TLSv1 ") || strings.Contains(config, "TLSv1.1") {
		t.Fatal("устаревшие версии TLS недопустимы")
	}
	for _, args := range [][3]string{{"a; rm -rf /", "/etc/x", "127.0.0.1:1"}, {"a.internal", "relative/dir", "127.0.0.1:1"},
		{"a.internal", "/etc/x;evil", "127.0.0.1:1"}, {"a.internal", "/etc/x", "127.0.0.1:1 {"}, {"", "/etc/x", "127.0.0.1:1"}} {
		if _, err := NginxConfig(args[0], args[1], args[2]); err == nil {
			t.Errorf("вредные параметры %v должны отвергаться", args)
		}
	}
}

// OPS-02: автоматическое продление. Действующая пара не трогается, пока срок
// не подошёл, продлевается при подходе срока, смене имён и порче, а сбой
// выпуска оставляет прежнюю пару.
func TestRenewOnlyWhenNeeded(t *testing.T) {
	ca, roots := newCA(t)
	store := Store{Dir: t.TempDir()}
	hosts := []string{"calculator.internal", "10.20.30.40"}
	const before = 30 * 24 * time.Hour

	renewed, first, err := store.Renew(ca, hosts, before, now)
	if err != nil || !renewed {
		t.Fatalf("первая установка: %v %v", renewed, err)
	}
	liveFirst := store.Live()
	renewed, _, err = store.Renew(ca, hosts, before, now.Add(24*time.Hour))
	if err != nil || renewed || store.Live() != liveFirst {
		t.Fatalf("свежая пара не должна меняться: %v %v", renewed, err)
	}
	renewed, info, err := store.Renew(ca, hosts, before, first.NotAfter.Add(-29*24*time.Hour))
	if err != nil || !renewed || store.Live() == liveFirst {
		t.Fatalf("за 29 дней до конца пара должна продлеваться: %v %v", renewed, err)
	}
	if !info.NotAfter.After(first.NotAfter) {
		t.Fatalf("новая пара должна действовать дольше: %v и %v", info.NotAfter, first.NotAfter)
	}
	// Смена имён — новый выпуск, даже если срок далёк.
	renewed, _, err = store.Renew(ca, append(hosts, "extra.internal"), before, now.Add(48*time.Hour))
	if err != nil || !renewed {
		t.Fatalf("новое имя требует нового сертификата: %v %v", renewed, err)
	}
	// Испорченный файл — тоже повод выпустить заново.
	if err := os.WriteFile(filepath.Join(store.Live(), "privkey.pem"), []byte("мусор"), 0o600); err != nil {
		t.Fatal(err)
	}
	renewed, _, err = store.Renew(ca, hosts, before, now.Add(72*time.Hour))
	if err != nil || !renewed {
		t.Fatalf("испорченная пара должна заменяться: %v %v", renewed, err)
	}
	if _, expiring, err := store.Status("calculator.internal", roots, before, now.Add(72*time.Hour)); err != nil || expiring {
		t.Fatalf("после продления срок не должен быть близким: %v %v", expiring, err)
	}
	// Центр, который не может выпустить, оставляет действующую пару.
	before2 := store.Live()
	if _, _, err := store.Renew(PEM{Cert: ca.Cert, Key: []byte("нет ключа")}, hosts, 10*365*24*time.Hour, now.Add(96*time.Hour)); err == nil {
		t.Fatal("выпуск без ключа центра должен завершаться ошибкой")
	}
	if store.Live() != before2 {
		t.Fatal("сбой выпуска не должен менять действующую пару")
	}
	if _, _, err := (Store{Dir: t.TempDir()}).Renew(ca, nil, before, now); err == nil {
		t.Fatal("без имён выпускать нечего")
	}
}

func TestStatusReportsExpiryAndMissingPair(t *testing.T) {
	ca, roots := newCA(t)
	store := Store{Dir: t.TempDir()}
	if _, _, err := store.Status("calculator.internal", roots, time.Hour, now); err == nil {
		t.Fatal("без установленной пары статус — ошибка")
	}
	_, info, err := store.Renew(ca, []string{"calculator.internal"}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, expiring, err := store.Status("calculator.internal", roots, 30*24*time.Hour, now); err != nil || expiring {
		t.Fatalf("свежая пара: %v %v", expiring, err)
	}
	if _, expiring, err := store.Status("calculator.internal", roots, 30*24*time.Hour, info.NotAfter.Add(-10*24*time.Hour)); err != nil || !expiring {
		t.Fatalf("за 10 дней до конца пара должна помечаться: %v %v", expiring, err)
	}
}

// OPS-02: TLS-надстройка compose не должна менять поведение без себя и должна
// иметь все части, без которых HTTPS не поднимется или не продлится.
func TestComposeTLSOverlayIsWiredConsistently(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	base := read("nginx/nginx.conf")
	for _, want := range []string{"include /etc/nginx/tls-http/*.conf;", "include /etc/nginx/tls-server/*.conf;"} {
		if !strings.Contains(base, want) {
			t.Errorf("nginx.conf должен подключать необязательные файлы TLS: нет %q", want)
		}
	}
	server := read("nginx/tls/server.conf")
	for _, want := range []string{"listen 443 ssl;", "/etc/nginx/tls/live/fullchain.pem", "/etc/nginx/tls/live/privkey.pem", "TLSv1.2 TLSv1.3", "Strict-Transport-Security", "return 308"} {
		if !strings.Contains(server, want) {
			t.Errorf("server.conf: нет %q", want)
		}
	}
	if strings.Contains(server, "TLSv1 ") || strings.Contains(server, "TLSv1.1") || strings.Contains(server, "SSLv3") {
		t.Error("устаревшие протоколы не допускаются")
	}
	// Проверки живости и версия обязаны оставаться доступными по HTTP.
	http := read("nginx/tls/http.conf")
	for _, want := range []string{"api/(ready|health)", "version", "$tls_redirect"} {
		if !strings.Contains(http, want) {
			t.Errorf("http.conf: нет %q", want)
		}
	}
	compose := read("docker-compose.tls.yml")
	for _, want := range []string{"tls-init:", "tls-issue:", "tls-renew:", "renew-loop", "TLS_HOST:?", "TLS_CA_DIR:?",
		"service_completed_successfully", "tls_data:/etc/nginx/tls:ro", "/etc/nginx/tls-http/tls.conf", "/etc/nginx/tls-server/tls.conf", "nginx -s reload", ":/ca:ro"} {
		if !strings.Contains(compose, want) {
			t.Errorf("docker-compose.tls.yml: нет %q", want)
		}
	}
	// Ключ центра доступен только на чтение, а контейнеры продления без лишних прав.
	if strings.Count(compose, "cap_drop: [ALL]") < 2 || strings.Count(compose, "read_only: true") < 2 {
		t.Error("контейнеры выпуска и продления должны работать без привилегий и с read-only корнем")
	}
	docker := read("backend/Dockerfile")
	if !strings.Contains(docker, "/out/tlsbootstrap") || !strings.Contains(docker, "COPY --from=builder /out/tlsbootstrap /app/tlsbootstrap") {
		t.Error("образ должен содержать tlsbootstrap: без него надстройка не выпускает сертификат")
	}
}
