// Package tlsboot готовит TLS для закрытого контура (OPS-02): локальный
// удостоверяющий центр, выпуск серверного сертификата, импорт готового
// сертификата, безопасная замена с откатом и конфигурация nginx с
// перенаправлением HTTP на HTTPS.
//
// Всё, что попадает в рабочий каталог, сначала проверяется: сертификат
// подходит к ключу, покрывает имя хоста, не просрочен, подписан достаточно
// стойким алгоритмом и строится до доверенного корня. Непроверенная пара
// рабочий каталог не затрагивает.
package tlsboot

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Пределы сроков: браузеры не принимают серверные сертификаты дольше ~398 дней.
const (
	MaxServerValidity = 398 * 24 * time.Hour
	MaxCAValidity     = 10 * 365 * 24 * time.Hour
	KeepVersions      = 3
)

// PEM — пара сертификат и ключ в PEM.
type PEM struct{ Cert, Key []byte }

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

func encodeKey(key crypto.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func encodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// NewCA создаёт локальный удостоверяющий центр. Он подписывает только
// конечные сертификаты (MaxPathLen 0) и не может выпускать промежуточные.
func NewCA(commonName string, validity time.Duration, now time.Time) (PEM, error) {
	if strings.TrimSpace(commonName) == "" {
		return PEM{}, errors.New("tlsboot: имя удостоверяющего центра не задано")
	}
	if validity <= 0 || validity > MaxCAValidity {
		return PEM{}, fmt.Errorf("tlsboot: срок центра от 1 дня до %d лет", int(MaxCAValidity.Hours()/24/365))
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return PEM{}, err
	}
	sn, err := serial()
	if err != nil {
		return PEM{}, err
	}
	template := &x509.Certificate{
		SerialNumber: sn, Subject: pkix.Name{CommonName: commonName},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.Add(validity),
		IsCA: true, BasicConstraintsValid: true, MaxPathLen: 0, MaxPathLenZero: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return PEM{}, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return PEM{}, err
	}
	return PEM{Cert: encodeCert(der), Key: keyPEM}, nil
}

func parseCA(ca PEM) (*x509.Certificate, crypto.Signer, error) {
	block, _ := pem.Decode(ca.Cert)
	if block == nil {
		return nil, nil, errors.New("tlsboot: сертификат центра не читается")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, err
	}
	if !cert.IsCA {
		return nil, nil, errors.New("tlsboot: это не сертификат удостоверяющего центра")
	}
	signer, err := parsePrivateKey(ca.Key)
	if err != nil {
		return nil, nil, err
	}
	return cert, signer, nil
}

func parsePrivateKey(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("tlsboot: ключ не читается")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("tlsboot: формат ключа не поддерживается")
}

// IssueServer выпускает серверный сертификат на перечисленные имена и адреса.
func IssueServer(ca PEM, hosts []string, validity time.Duration, now time.Time) (PEM, error) {
	if len(hosts) == 0 {
		return PEM{}, errors.New("tlsboot: укажите хотя бы одно имя или адрес")
	}
	if validity <= 0 || validity > MaxServerValidity {
		return PEM{}, fmt.Errorf("tlsboot: срок серверного сертификата от 1 до %d дней", int(MaxServerValidity.Hours()/24))
	}
	caCert, caKey, err := parseCA(ca)
	if err != nil {
		return PEM{}, err
	}
	if now.After(caCert.NotAfter) {
		return PEM{}, errors.New("tlsboot: срок удостоверяющего центра истёк")
	}
	notAfter := now.Add(validity)
	if notAfter.After(caCert.NotAfter) {
		return PEM{}, errors.New("tlsboot: сертификат не может жить дольше удостоверяющего центра")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return PEM{}, err
	}
	sn, err := serial()
	if err != nil {
		return PEM{}, err
	}
	template := &x509.Certificate{
		SerialNumber: sn, Subject: pkix.Name{CommonName: hosts[0]},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" || strings.ContainsAny(host, " /\\") {
			return PEM{}, fmt.Errorf("tlsboot: недопустимое имя %q", host)
		}
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, strings.ToLower(host))
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return PEM{}, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return PEM{}, err
	}
	// Сертификат отдаётся вместе с сертификатом центра: клиент строит цепочку сам.
	return PEM{Cert: append(encodeCert(der), ca.Cert...), Key: keyPEM}, nil
}

// Info — сведения о проверенной паре.
type Info struct {
	Subject   string
	Hosts     []string
	NotAfter  time.Time
	Chain     int
	Algorithm string
}

func parseChain(data []byte) ([]*x509.Certificate, error) {
	var chain []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("tlsboot: сертификат не читается: %w", err)
		}
		chain = append(chain, cert)
	}
	if len(chain) == 0 {
		return nil, errors.New("tlsboot: в файле нет сертификатов")
	}
	return chain, nil
}

func strongKey(pub crypto.PublicKey) (string, error) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			return "", fmt.Errorf("tlsboot: RSA-ключ короче 2048 бит (%d)", k.N.BitLen())
		}
		return fmt.Sprintf("RSA-%d", k.N.BitLen()), nil
	case *ecdsa.PublicKey:
		if k.Curve != elliptic.P256() && k.Curve != elliptic.P384() {
			return "", errors.New("tlsboot: допустимы кривые P-256 и P-384")
		}
		return "ECDSA-" + k.Curve.Params().Name, nil
	case ed25519.PublicKey:
		return "Ed25519", nil
	}
	return "", errors.New("tlsboot: тип ключа не поддерживается")
}

// Validate проверяет пару перед установкой. roots — доверенные корни; пусто
// означает системные (подходит для сертификата публичного центра).
func Validate(pair PEM, host string, roots *x509.CertPool, now time.Time) (Info, error) {
	chain, err := parseChain(pair.Cert)
	if err != nil {
		return Info{}, err
	}
	leaf := chain[0]
	signer, err := parsePrivateKey(pair.Key)
	if err != nil {
		return Info{}, err
	}
	algorithm, err := strongKey(leaf.PublicKey)
	if err != nil {
		return Info{}, err
	}
	if !publicKeysEqual(leaf.PublicKey, signer.Public()) {
		return Info{}, errors.New("tlsboot: ключ не подходит к сертификату")
	}
	switch leaf.SignatureAlgorithm {
	case x509.MD5WithRSA, x509.SHA1WithRSA, x509.ECDSAWithSHA1, x509.DSAWithSHA1, x509.MD2WithRSA:
		return Info{}, fmt.Errorf("tlsboot: устаревший алгоритм подписи %s", leaf.SignatureAlgorithm)
	}
	if now.Before(leaf.NotBefore) {
		return Info{}, errors.New("tlsboot: сертификат ещё не действует")
	}
	if now.After(leaf.NotAfter) {
		return Info{}, errors.New("tlsboot: срок сертификата истёк")
	}
	if host != "" {
		if err := leaf.VerifyHostname(host); err != nil {
			return Info{}, fmt.Errorf("tlsboot: сертификат не покрывает %s: %w", host, err)
		}
	}
	intermediates := x509.NewCertPool()
	for _, cert := range chain[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, CurrentTime: now,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		return Info{}, fmt.Errorf("tlsboot: цепочка не строится до доверенного корня: %w", err)
	}
	hosts := append([]string{}, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		hosts = append(hosts, ip.String())
	}
	sort.Strings(hosts)
	return Info{Subject: leaf.Subject.String(), Hosts: hosts, NotAfter: leaf.NotAfter, Chain: len(chain), Algorithm: algorithm}, nil
}

func publicKeysEqual(a, b crypto.PublicKey) bool {
	type equaler interface{ Equal(crypto.PublicKey) bool }
	if eq, ok := a.(equaler); ok {
		return eq.Equal(b)
	}
	return false
}

// Pool собирает пул корней из PEM.
func Pool(data []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("tlsboot: в файле корней нет сертификатов")
	}
	return pool, nil
}

// Store — каталог с версиями пары и указателем live на действующую.
type Store struct{ Dir string }

func (s Store) versions() string { return filepath.Join(s.Dir, "versions") }
func (s Store) live() string     { return filepath.Join(s.Dir, "live") }

// Live возвращает путь действующей версии (пусто, если не установлена).
func (s Store) Live() string {
	target, err := os.Readlink(s.live())
	if err != nil {
		return ""
	}
	return filepath.Join(s.Dir, target)
}

// Install проверяет пару и делает её действующей. Проверка идёт до записи, а
// переключение — одной атомарной подменой указателя: nginx видит либо старую
// пару целиком, либо новую целиком. Прежняя версия остаётся для отката.
func (s Store) Install(pair PEM, host string, roots *x509.CertPool, now time.Time) (Info, error) {
	info, err := Validate(pair, host, roots, now)
	if err != nil {
		return Info{}, err
	}
	if err := os.MkdirAll(s.versions(), 0o700); err != nil {
		return Info{}, err
	}
	name := fmt.Sprintf("%s-%d", now.UTC().Format("20060102T150405Z"), now.UnixNano()%1000000)
	dir := filepath.Join(s.versions(), name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Info{}, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	if err := writeSynced(filepath.Join(dir, "fullchain.pem"), pair.Cert, 0o644); err != nil {
		cleanup()
		return Info{}, err
	}
	if err := writeSynced(filepath.Join(dir, "privkey.pem"), pair.Key, 0o600); err != nil {
		cleanup()
		return Info{}, err
	}
	if err := s.point(filepath.Join("versions", name)); err != nil {
		cleanup()
		return Info{}, err
	}
	s.prune()
	return info, nil
}

func writeSynced(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// point атомарно переключает live на относительный путь target.
func (s Store) point(target string) error {
	tmp := filepath.Join(s.Dir, fmt.Sprintf(".live-%d", time.Now().UnixNano()))
	if err := os.Symlink(target, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.live()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (s Store) list() []string {
	entries, err := os.ReadDir(s.versions())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// prune оставляет последние версии, действующую — всегда.
func (s Store) prune() {
	names := s.list()
	live := filepath.Base(s.Live())
	for len(names) > KeepVersions {
		if names[0] != live {
			os.RemoveAll(filepath.Join(s.versions(), names[0]))
		}
		names = names[1:]
	}
}

// Rollback возвращает предыдущую версию. Возвращённая пара проверяется заново:
// за время между заменой и откатом она могла истечь.
func (s Store) Rollback(host string, roots *x509.CertPool, now time.Time) (Info, error) {
	names := s.list()
	live := filepath.Base(s.Live())
	previous := ""
	for _, name := range names {
		if name == live {
			break
		}
		previous = name
	}
	if previous == "" {
		return Info{}, errors.New("tlsboot: предыдущей версии нет")
	}
	cert, err := os.ReadFile(filepath.Join(s.versions(), previous, "fullchain.pem"))
	if err != nil {
		return Info{}, err
	}
	key, err := os.ReadFile(filepath.Join(s.versions(), previous, "privkey.pem"))
	if err != nil {
		return Info{}, err
	}
	info, err := Validate(PEM{Cert: cert, Key: key}, host, roots, now)
	if err != nil {
		return Info{}, fmt.Errorf("tlsboot: предыдущая версия непригодна: %w", err)
	}
	return info, s.point(filepath.Join("versions", previous))
}

// NginxConfig возвращает конфигурацию сервера: HTTP перенаправляется на HTTPS
// (308 сохраняет метод), TLS 1.2+, HSTS, сертификаты берутся по указателю live.
func NginxConfig(host, certDir, upstream string) (string, error) {
	if strings.ContainsAny(host, " ;{}\"'\\\n") || host == "" {
		return "", fmt.Errorf("tlsboot: недопустимое имя хоста %q", host)
	}
	if strings.ContainsAny(certDir, " ;{}\"'\\\n") || !filepath.IsAbs(certDir) {
		return "", fmt.Errorf("tlsboot: каталог сертификатов должен быть абсолютным путём без служебных символов")
	}
	if strings.ContainsAny(upstream, " ;{}\"'\\\n") || upstream == "" {
		return "", fmt.Errorf("tlsboot: недопустимый адрес приложения %q", upstream)
	}
	return fmt.Sprintf(`# Создано tlsbootstrap. Ставится на хост-nginx вне Docker.
server {
    listen 80;
    server_name %[1]s;
    return 308 https://%[1]s$request_uri;
}

server {
    listen 443 ssl;
    server_name %[1]s;
    ssl_certificate     %[2]s/live/fullchain.pem;
    ssl_certificate_key %[2]s/live/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_timeout 1h;
    ssl_session_tickets off;
    server_tokens off;
    add_header Strict-Transport-Security "max-age=31536000" always;
    client_max_body_size 21m;
    location / {
        proxy_pass http://%[3]s;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_read_timeout 120s;
    }
}
`, host, certDir, upstream), nil
}
