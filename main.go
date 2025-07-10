package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
	"gopkg.in/telebot.v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Config struct {
	TelegramToken string
	OpenAIAPIKey  string
	OpenAIBaseURL string
	DatabasePath  string
	Port          string
	BotUsername   string
}

type Message struct {
	ID        uint  `gorm:"primaryKey"`
	ChatID    int64 `gorm:"index"`
	UserID    int64 `gorm:"index"`
	Username  string
	Text      string    `gorm:"type:text"`
	Timestamp time.Time `gorm:"index"`
	CreatedAt time.Time
}

type ChatSummary struct {
	ID        uint      `gorm:"primaryKey"`
	ChatID    int64     `gorm:"index"`
	Date      time.Time `gorm:"index"`
	Summary   string    `gorm:"type:text"`
	CreatedAt time.Time
}

type Bot struct {
	telebot *telebot.Bot
	db      *gorm.DB
	openai  *openai.Client
	config  *Config
}

func loadConfig() *Config {
	return &Config{
		TelegramToken: getEnv("TELEGRAM_BOT_TOKEN", ""),
		OpenAIAPIKey:  getEnv("OPENAI_API_KEY", ""),
		OpenAIBaseURL: getEnv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		DatabasePath:  getEnv("DATABASE_PATH", "./summarybot.db"),
		Port:          getEnv("PORT", "8080"),
		BotUsername:   getEnv("BOT_USERNAME", "zagichak_bot"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func initDatabase(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&Message{}, &ChatSummary{})
	if err != nil {
		return nil, err
	}

	return db, nil
}

func initOpenAI(config *Config) *openai.Client {
	clientConfig := openai.DefaultConfig(config.OpenAIAPIKey)
	if config.OpenAIBaseURL != "" {
		clientConfig.BaseURL = config.OpenAIBaseURL
	}
	return openai.NewClientWithConfig(clientConfig)
}

func (b *Bot) saveMessage(m *telebot.Message) {
	if m.Text == "" {
		return
	}

	message := Message{
		ChatID:    m.Chat.ID,
		UserID:    m.Sender.ID,
		Username:  m.Sender.Username,
		Text:      m.Text,
		Timestamp: time.Unix(m.Unixtime, 0),
	}

	b.db.Create(&message)
}

func (b *Bot) getMessagesForPeriod(chatID int64, days int) ([]Message, error) {
	var messages []Message
	startDate := time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)
	endDate := startDate.Add(24 * time.Hour)

	err := b.db.Where("chat_id = ? AND timestamp >= ? AND timestamp < ?",
		chatID, startDate, endDate).
		Order("timestamp ASC").
		Find(&messages).Error

	return messages, err
}

func (b *Bot) generateSummary(messages []Message, period string) (string, error) {
	if len(messages) == 0 {
		return fmt.Sprintf("За %s не было интересных сообщений 🤷‍♂️", period), nil
	}

	var textBuilder strings.Builder
	for _, msg := range messages {
		textBuilder.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			msg.Timestamp.Format("15:04"), msg.Username, msg.Text))
	}

	prompt := fmt.Sprintf(`Проанализируй сообщения из Telegram чата за %s и создай краткое резюме самых интересных моментов на русском языке. 

Сообщения:
%s

Требования к резюме:
- Выдели 3-5 самых интересных/важных тем или событий
- Используй эмодзи для лучшего восприятия
- Будь кратким, но информативным
- Пиши в неформальном стиле
- Если есть важные ссылки или упоминания, включи их
- Если ничего особенно интересного не было, скажи об этом честно

Формат ответа: просто текст резюме без дополнительных пояснений.`,
		period, textBuilder.String())

	resp, err := b.openai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   500,
			Temperature: 0.7,
		},
	)

	if err != nil {
		return "", fmt.Errorf("ошибка OpenAI API: %v", err)
	}

	if len(resp.Choices) == 0 {
		return "Не удалось создать резюме 😞", nil
	}

	return resp.Choices[0].Message.Content, nil
}

func (b *Bot) handleSummaryRequest(c telebot.Context) error {
	message := c.Message()

	if !strings.Contains(message.Text, "@"+b.config.BotUsername) {
		return nil
	}

	text := strings.ToLower(message.Text)
	var days int
	var period string

	if strings.Contains(text, "сегодня") {
		days = 0
		period = "сегодня"
	} else if strings.Contains(text, "вчера") {
		days = 1
		period = "вчера"
	} else if strings.Contains(text, "позавчера") {
		days = 2
		period = "позавчера"
	} else {
		re := regexp.MustCompile(`(\d+)\s*дн`)
		matches := re.FindStringSubmatch(text)
		if len(matches) > 1 {
			if d, err := strconv.Atoi(matches[1]); err == nil && d <= 7 {
				days = d
				period = fmt.Sprintf("%d дней назад", d)
			} else {
				return c.Reply("Могу показать резюме только за последние 7 дней 📅")
			}
		} else {
			return c.Reply("Напиши '@zagichak_bot что было за сегодня/вчера/позавчера' или '@zagichak_bot что было за N дней' (макс 7)")
		}
	}

	statusMsg, _ := c.Bot().Send(c.Chat(), "Генерирую резюме... ⏳")

	messages, err := b.getMessagesForPeriod(c.Chat().ID, days)
	if err != nil {
		c.Bot().Delete(statusMsg)
		return c.Reply("Ошибка при получении сообщений 😞")
	}

	summary, err := b.generateSummary(messages, period)
	if err != nil {
		c.Bot().Delete(statusMsg)
		log.Printf("Ошибка генерации резюме: %v", err)
		return c.Reply("Не удалось создать резюме. Попробуй позже 🤖")
	}

	chatSummary := ChatSummary{
		ChatID:  c.Chat().ID,
		Date:    time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour),
		Summary: summary,
	}
	b.db.Create(&chatSummary)

	// Удаляем статус и отправляем резюме
	c.Bot().Delete(statusMsg)

	summaryText := fmt.Sprintf("📋 **Резюме за %s**\n\n%s\n\n_Проанализировано сообщений: %d_",
		period, summary, len(messages))

	return c.Reply(summaryText, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
	})
}

func (b *Bot) handleStart(c telebot.Context) error {
	welcomeText := `Привет! 👋 

Я бот для создания резюме чата. Просто упомяни меня и скажи:
• @zagichak_bot что было за сегодня
• @zagichak_bot что было за вчера  
• @zagichak_bot что было за позавчера
• @zagichak_bot что было за 3 дня

Я проанализирую сообщения и выдам самое интересное! 🤖✨`

	return c.Reply(welcomeText)
}

func (b *Bot) startHealthServer() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	log.Printf("Health server запущен на порту %s", b.config.Port)
	if err := http.ListenAndServe(":"+b.config.Port, nil); err != nil {
		log.Printf("Ошибка health сервера: %v", err)
	}
}

func main() {
	config := loadConfig()

	db, err := initDatabase(config.DatabasePath)
	if err != nil {
		log.Fatalf("Ошибка инициализации БД: %v", err)
	}

	openaiClient := initOpenAI(config)

	pref := telebot.Settings{
		Token:  config.TelegramToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	tgBot, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatalf("Ошибка создания Telegram бота: %v", err)
	}

	bot := &Bot{
		telebot: tgBot,
		db:      db,
		openai:  openaiClient,
		config:  config,
	}

	tgBot.Handle("/start", bot.handleStart)
	tgBot.Handle(telebot.OnText, func(c telebot.Context) error {
		bot.saveMessage(c.Message())

		if strings.Contains(c.Message().Text, "@"+config.BotUsername) {
			return bot.handleSummaryRequest(c)
		}

		return nil
	})

	go bot.startHealthServer()

	log.Printf("Бот запущен! Username: @%s", config.BotUsername)
	tgBot.Start()
}
