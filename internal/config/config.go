package config

import (
	"log/slog"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DbPort          string
	DbUser          string
	DbPassword      string
	DbName          string
	DbHost          string
	DbAdminUser     string
	DbAdminPassword string
	LlmApiKey       string
	LlmBaseUrl      string
	LlmModel        string
	BotToken        string
	HttpPort        string
}

func getenv(key, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}

func LoadConfig(fileName string) *Config {
	if err := godotenv.Load(fileName); err != nil {
		slog.Info("no .env file, using environment", "file", fileName)
	}
	cfg := &Config{
		DbPort:          getenv("DB_PORT", "5432"),
		DbUser:          getenv("DB_USER", "expenses_app"),
		DbPassword:      getenv("DB_PASSWORD", ""),
		DbName:          getenv("DB_NAME", "server"),
		DbHost:          getenv("DB_HOST", "localhost"),
		DbAdminUser:     getenv("DB_ADMIN_USER", "postgres"),
		DbAdminPassword: getenv("DB_ADMIN_PASSWORD", ""),
		LlmApiKey:       getenv("LLM_API_KEY", ""),
		LlmBaseUrl:      getenv("LLM_BASE_URL", "https://openrouter.ai/api/v1"),
		LlmModel:        getenv("LLM_MODEL", "auto"),
		BotToken:        getenv("BOT_TOKEN", ""),
		HttpPort:        getenv("HTTP_PORT", "8080"),
	}
	for k, v := range map[string]string{"DB_PASSWORD": cfg.DbPassword, "LLM_API_KEY": cfg.LlmApiKey, "BOT_TOKEN": cfg.BotToken} {
		if v == "" {
			slog.Warn("config value is empty", "key", k)
		}
	}
	return cfg
}
