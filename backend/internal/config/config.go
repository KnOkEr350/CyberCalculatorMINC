// Package config читает конфигурацию приложения из переменных окружения.
package config

import (
	"fmt"
	"os"
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
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() Config {
	return Config{
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
	}
}

func (c Config) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode,
	)
}
