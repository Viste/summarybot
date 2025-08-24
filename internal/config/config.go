package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	TelegramToken    string
	OpenAIAPIKey     string
	OpenAIBaseURL    string
	DatabasePath     string
	Port             string
	BotUsername      string
	AllowedChats     []int64
	AdminUserIDs     []int64
	RequireApproval  bool
	OpenAIModel      string
	MaxTokens        int
	MinMessagesForAI int
}

func Load() *Config {
	maxTokens := 1200
	if tokensStr := getEnv("OPENAI_MAX_TOKENS", ""); tokensStr != "" {
		if parsed, err := strconv.Atoi(tokensStr); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	minMessages := 20
	if minStr := getEnv("MIN_MESSAGES_FOR_AI", ""); minStr != "" {
		if parsed, err := strconv.Atoi(minStr); err == nil && parsed > 0 {
			minMessages = parsed
		}
	}

	return &Config{
		TelegramToken:    getEnv("TELEGRAM_BOT_TOKEN", ""),
		OpenAIAPIKey:     getEnv("OPENAI_API_KEY", ""),
		OpenAIBaseURL:    getEnv("OPENAI_BASE_URL", "http://31.172.78.152:9000/v1"),
		DatabasePath:     getEnv("DATABASE_PATH", "./summarybot.db"),
		Port:             getEnv("PORT", "8080"),
		BotUsername:      getEnv("BOT_USERNAME", "zagichak_bot"),
		AllowedChats:     parseInt64List(getEnv("ALLOWED_CHATS", "")),
		AdminUserIDs:     parseInt64List(getEnv("ADMIN_USER_IDS", "")),
		RequireApproval:  getEnv("REQUIRE_APPROVAL", "true") == "true",
		OpenAIModel:      getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		MaxTokens:        maxTokens,
		MinMessagesForAI: minMessages,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseInt64List(str string) []int64 {
	if str == "" {
		return []int64{}
	}

	parts := strings.Split(str, ",")
	result := make([]int64, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if id, err := strconv.ParseInt(part, 10, 64); err == nil {
			result = append(result, id)
		}
	}

	return result
}
