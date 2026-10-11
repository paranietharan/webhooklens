package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port              string
	DatabaseURL       string
	RedisURL          string
	JWTSecret         string
	JWTExpiration     time.Duration
	WorkerPoolSize    int
	WorkerBufferSize  int
	MaxRequestBodyMB  int64
	CacheTTL          time.Duration
	SMTPHost          string
	SMTPPort          int
	SMTPFrom          string
	AppBaseURL        string
}

func Load() *Config {
	return &Config{
		Port:             getEnv("PORT", "8080"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://webhooklens:webhooklens_secret@localhost:5432/webhooklens?sslmode=disable"),
		RedisURL:         getEnv("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:        getEnv("JWT_SECRET", "webhooklens-super-secret-developer-jwt-signing-key-32b!"),
		JWTExpiration:    getDurationEnv("JWT_EXPIRATION_HOURS", 24) * time.Hour,
		WorkerPoolSize:   getIntEnv("WORKER_POOL_SIZE", 10),
		WorkerBufferSize: getIntEnv("WORKER_BUFFER_SIZE", 5000),
		MaxRequestBodyMB: int64(getIntEnv("MAX_REQUEST_BODY_MB", 5)),
		CacheTTL:         getDurationEnv("ENDPOINT_CACHE_TTL_MINUTES", 5) * time.Minute,
		SMTPHost:         getEnv("SMTP_HOST", "localhost"),
		SMTPPort:         getIntEnv("SMTP_PORT", 1025),
		SMTPFrom:         getEnv("SMTP_FROM", "noreply@webhooklens.local"),
		AppBaseURL:       getEnv("APP_BASE_URL", "http://localhost:8080"),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getIntEnv(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getDurationEnv(key string, defaultVal int) time.Duration {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return time.Duration(i)
		}
	}
	return time.Duration(defaultVal)
}
