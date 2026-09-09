// Package config читает конфигурацию приложения из переменных окружения.
package config

import (
	"cybercalc/internal/auth"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	HTTPAddr          string
	UploadDir         string
	SessionTTLh       int
	AdminBootEmail    string
	AdminBootPassword string
	PublicURL         string
	Environment       string
	CookieSecure      bool
	ScannerAddress    string
	UploadQuotaBytes  int64
	MFAKey            string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() Config {
	c := Config{
		DBHost:     getenv("DB_HOST", "db"),
		DBPort:     getenv("DB_PORT", "5432"),
		DBUser:     getenv("DB_USER", "cybercalc"),
		DBPassword: getenv("DB_PASSWORD", "cybercalc"),
		DBName:     getenv("DB_NAME", "cybercalc"),
		DBSSLMode:  getenv("DB_SSLMODE", "disable"),

		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		UploadDir:   getenv("UPLOAD_DIR", "/data/uploads"),
		SessionTTLh: 12,

		AdminBootEmail:    getenv("ADMIN_BOOTSTRAP_EMAIL", "admin@minc.local"),
		AdminBootPassword: getenv("ADMIN_BOOTSTRAP_PASSWORD", "ChangeMe123!"),
		PublicURL:         strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"),
		Environment:       getenv("APP_ENV", "development"),
	}
	c.AdminBootEmail = strings.ToLower(strings.TrimSpace(c.AdminBootEmail))
	if raw := os.Getenv("SESSION_TTL_HOURS"); raw != "" {
		c.SessionTTLh, _ = strconv.Atoi(raw)
	}
	c.CookieSecure = strings.HasPrefix(c.PublicURL, "https://")
	c.ScannerAddress = os.Getenv("CLAMAV_ADDRESS")
	c.MFAKey = os.Getenv("MFA_ENCRYPTION_KEY")
	c.UploadQuotaBytes = 1 << 30
	if raw := os.Getenv("UPLOAD_QUOTA_BYTES"); raw != "" {
		c.UploadQuotaBytes, _ = strconv.ParseInt(raw, 10, 64)
	}
	return c
}

func (c Config) DSN() string {
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.DBHost, c.DBPort), User: url.UserPassword(c.DBUser, c.DBPassword), Path: "/" + c.DBName}
	q := u.Query()
	q.Set("sslmode", c.DBSSLMode)
	q.Set("connect_timeout", "5")
	q.Set("statement_timeout", "60000")
	q.Set("lock_timeout", "10000")
	q.Set("idle_in_transaction_session_timeout", "90000")
	u.RawQuery = q.Encode()
	return u.String()
}

func (c Config) Validate() error {
	if c.UploadQuotaBytes < 64<<20 || c.UploadQuotaBytes > 1<<40 {
		return fmt.Errorf("UPLOAD_QUOTA_BYTES должен быть от 64 МиБ до 1 ТиБ")
	}
	if c.SessionTTLh < 1 || c.SessionTTLh > 24 {
		return fmt.Errorf("SESSION_TTL_HOURS должен быть от 1 до 24")
	}
	if _, err := mail.ParseAddress(c.AdminBootEmail); err != nil {
		return fmt.Errorf("некорректный ADMIN_BOOTSTRAP_EMAIL")
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("PUBLIC_URL должен содержать только http(s)://host[:port]")
		}
	}
	if c.Environment == "production" {
		if _, err := auth.MFAKey(c.MFAKey); err != nil {
			return err
		}
		if c.ScannerAddress == "" {
			return fmt.Errorf("production требует CLAMAV_ADDRESS для проверки документов")
		}
		if !c.CookieSecure {
			return fmt.Errorf("для production необходим PUBLIC_URL=https://домен и TLS на внешнем прокси")
		}
		for _, password := range []string{c.DBPassword, c.AdminBootPassword} {
			if len(password) < 16 || password == "change_me_db_password" || password == "ChangeMe123!" || password == "local-only-runtime-password" {
				return fmt.Errorf("production требует уникальные пароли БД и bootstrap длиной минимум 16 символов")
			}
		}
	}
	return nil
}
