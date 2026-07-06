package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	WebhookSecret string
	AppID         int64
	AppPrivateKey []byte
	GitUserName   string
	GitUserEmail  string
	ListenAddr    string
	LogLevel      string
}

func Load() (*Config, error) {
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		return nil, fmt.Errorf("GITHUB_APP_ID is required")
	}
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("GITHUB_APP_ID must be numeric: %w", err)
	}

	privateKey := os.Getenv("GITHUB_APP_PRIVATE_KEY")
	if privateKey == "" {
		return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY is required")
	}
	// Support literal \n sequences common when PEM is stored in env vars.
	privateKey = strings.ReplaceAll(privateKey, `\n`, "\n")

	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	if webhookSecret == "" {
		return nil, fmt.Errorf("WEBHOOK_SECRET is required")
	}

	listenAddr := os.Getenv("LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	return &Config{
		WebhookSecret: webhookSecret,
		AppID:         appID,
		AppPrivateKey: []byte(privateKey),
		ListenAddr:    listenAddr,
		LogLevel:      logLevel,
	}, nil
}
