package config

import (
	"os"
	"strconv"
	"time"
)

// Config contains runtime settings for E2E Engine
type Config struct {
	BaseURL       string
	NatsURL       string
	AdminLogin    string
	AdminPassword string
	ClientToken   string
	RestToken     string
	CourierToken  string
	AdminToken    string
	RequestTimeout time.Duration
	PollInterval  time.Duration

	// VerificationCode is the default SMS verification code accepted by the real backend
	VerificationCode string
	// DataDir is the working directory for persisted artifacts (user scenarios etc.)
	DataDir string
}

// LoadFromEnv loads configuration with sensible defaults and environment overrides
func LoadFromEnv() *Config {
	baseURL := getEnv("BASE_URL", "http://localhost:3000")
	natsURL := getEnv("NATS_URL", "nats://localhost:4222")
	adminLogin := getEnv("ADMIN_LOGIN", "")
	adminPassword := getEnv("ADMIN_PASSWORD", "")

	timeoutSec, _ := strconv.Atoi(getEnv("REQUEST_TIMEOUT_SEC", "15"))
	if timeoutSec <= 0 {
		timeoutSec = 15
	}

	return &Config{
		BaseURL:        baseURL,
		NatsURL:        natsURL,
		AdminLogin:     adminLogin,
		AdminPassword:  adminPassword,
		ClientToken:    os.Getenv("CLIENT_TOKEN"),
		RestToken:      os.Getenv("REST_TOKEN"),
		CourierToken:   os.Getenv("COURIER_TOKEN"),
		AdminToken:     os.Getenv("ADMIN_TOKEN"),
		RequestTimeout: time.Duration(timeoutSec) * time.Second,
		PollInterval:   500 * time.Millisecond,

		VerificationCode: getEnv("VERIFICATION_CODE", "1234"),
		DataDir:          getEnv("DATA_DIR", "./data"),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
