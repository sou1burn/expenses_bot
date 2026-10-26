package config

import (
	"github.com/joho/godotenv"
	"log"
	"os"
)

type Config struct {
	DbPort     string
	DbUser     string
	DbPassword string
	DbName     string
	DbHost     string
	LlmApiKey  string
	LlmBaseUrl string
	LlmModel   string
	BotToken   string
}

func getenv(key, def string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return def
}

func LoadConfig(fileName string) *Config {
	if err := godotenv.Load(fileName); err != nil {
		log.Println("No .env file found")
	}
	return &Config{
		DbPort:     getenv("DB_PORT", "5432"),
		DbUser:     getenv("DB_USER", "postgres"),
		DbPassword: getenv("DB_PASSWORD", ""),
		DbName:     getenv("DB_NAME", "server"),
		DbHost:     getenv("DB_HOST", "localhost"),
		LlmApiKey:  getenv("LLM_API_KEY", ""),
		LlmBaseUrl: getenv("LLM_BASE_URL", "https://api.groq.com/openai/v1"),
		LlmModel:   getenv("LLM_MODEL", "llama-3.3-70b-versatile"),
		BotToken:   getenv("BOT_TOKEN", ""),
	}
}
