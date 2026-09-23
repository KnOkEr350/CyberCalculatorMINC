package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readConfig(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "nginx", name))
	if err != nil {
		t.Fatalf("не прочитать конфигурацию %s: %v", name, err)
	}
	return string(body)
}

// OPS-03: укрепление периметра держится конфигурацией, а не памятью о том,
// что когда-то было настроено. Снятая директива — тихая регрессия, которую
// в рабочем контуре замечают уже по последствиям.
func TestNginxKeepsHardeningDirectives(t *testing.T) {
	config := readConfig(t, "nginx.conf")

	required := map[string]string{
		"server_tokens off":       "версия nginx не должна раскрываться",
		"limit_req_zone":          "без ограничения частоты запросов API открыт для перебора",
		"limit_conn_zone":         "без ограничения числа соединений один клиент занимает пул",
		"limit_req_status 429":    "превышение лимита должно отвечать 429, а не ошибкой сервера",
		"large_client_header_buf": "размер заголовков должен быть ограничен",
		"client_header_timeout":   "медленный заголовок не должен держать соединение",
		"client_body_timeout":     "медленное тело запроса не должно держать соединение",
	}
	for directive, why := range required {
		if !strings.Contains(config, directive) {
			t.Errorf("в конфигурации нет %q: %s", directive, why)
		}
	}

	// Заголовки безопасности ставятся всегда, включая ответы об ошибках:
	// без always nginx опускает их на 4xx/5xx.
	headers := []string{
		"X-Content-Type-Options nosniff",
		"X-Frame-Options DENY",
		"Referrer-Policy no-referrer",
		"Permissions-Policy",
		"Content-Security-Policy",
	}
	for _, header := range headers {
		index := strings.Index(config, header)
		if index < 0 {
			t.Errorf("не выставляется заголовок %s", header)
			continue
		}
		line := config[index:]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		if !strings.Contains(line, "always") {
			t.Errorf("заголовок %s выставляется без always и пропадёт на ответах об ошибках", header)
		}
	}

	// Политика содержимого не должна разрешать выполнение внешнего или
	// встроенного кода: это единственная защита от внедрения скрипта.
	csp := config[strings.Index(config, "Content-Security-Policy"):]
	if end := strings.IndexByte(csp, '\n'); end >= 0 {
		csp = csp[:end]
	}
	for _, forbidden := range []string{"unsafe-eval", "script-src 'self' 'unsafe-inline'", "script-src *"} {
		if strings.Contains(csp, forbidden) {
			t.Errorf("политика содержимого разрешает %q", forbidden)
		}
	}
	for _, expected := range []string{"object-src 'none'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, expected) {
			t.Errorf("в политике содержимого нет %q", expected)
		}
	}
}

// Базовая фильтрация: приложение использует ограниченный набор методов, а
// служебные файлы не публикуются никогда.
func TestNginxFiltersMethodsAndServiceFiles(t *testing.T) {
	config := readConfig(t, "nginx.conf")

	if !strings.Contains(config, "$request_method") {
		t.Fatal("методы запросов не фильтруются: TRACE и произвольные расширения доходят до backend")
	}
	methods := config[strings.Index(config, "$request_method"):]
	if end := strings.IndexByte(methods, '\n'); end >= 0 {
		methods = methods[:end]
	}
	for _, allowed := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if !strings.Contains(methods, allowed) {
			t.Errorf("метод %s используется приложением, но не разрешён", allowed)
		}
	}
	for _, denied := range []string{"TRACE", "CONNECT"} {
		if strings.Contains(methods, denied) {
			t.Errorf("метод %s не должен разрешаться", denied)
		}
	}
	if !strings.Contains(config, `location ~ /\.`) {
		t.Error("скрытые файлы (.git, .env) должны отдавать 404, а не проксироваться")
	}
	if !strings.Contains(config, "well-known") {
		t.Error("исключение для /.well-known нужно, иначе сломается выпуск сертификатов")
	}
}

// Вход защищён отдельно от остального API: подбор пароля не должен
// укладываться в общий лимит частоты.
func TestNginxProtectsLoginSeparately(t *testing.T) {
	config := readConfig(t, "nginx.conf")

	index := strings.Index(config, "location = /api/auth/login")
	if index < 0 {
		t.Fatal("вход не выделен в отдельную локацию и живёт по общему лимиту")
	}
	block := config[index:]
	if end := strings.Index(block, "location /"); end > 0 {
		block = block[:end]
	}
	if !strings.Contains(block, "login_limit") {
		t.Error("для входа не задан отдельный лимит частоты")
	}
	if !strings.Contains(block, "client_max_body_size") {
		t.Error("тело запроса на вход должно быть жёстко ограничено")
	}
	// Лимит входа должен быть строго жёстче общего: иначе он бессмыслен.
	loginRate := strings.Contains(config, "zone=login_limit:1m rate=6r/m")
	apiRate := strings.Contains(config, "zone=api_limit:10m rate=20r/s")
	if !loginRate || !apiRate {
		t.Fatal("лимиты входа и API должны быть заданы явно и различаться")
	}
}

// Пример TLS-конфигурации — часть поставки: по нему разворачивают рабочий
// контур, и слабые настройки разошлись бы вместе с ним.
func TestTLSExampleIsSafeToCopy(t *testing.T) {
	config := readConfig(t, "tls-host.conf.example")

	if !strings.Contains(config, "ssl_protocols TLSv1.2 TLSv1.3") {
		t.Error("разрешены должны быть только TLS 1.2 и 1.3")
	}
	for _, weak := range []string{"TLSv1.0", "TLSv1.1", "SSLv3"} {
		if strings.Contains(config, weak) {
			t.Errorf("пример разрешает устаревший протокол %s", weak)
		}
	}
	if !strings.Contains(config, "Strict-Transport-Security") {
		t.Error("в примере нет HSTS")
	}
	if !strings.Contains(config, "server_tokens off") {
		t.Error("пример раскрывает версию nginx")
	}
}
