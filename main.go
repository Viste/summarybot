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
	TelegramToken   string
	OpenAIAPIKey    string
	OpenAIBaseURL   string
	DatabasePath    string
	Port            string
	BotUsername     string
	AllowedChats    []int64
	AdminUserIDs    []int64
	RequireApproval bool
	OpenAIModel     string
	MaxTokens       int
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

type AllowedChat struct {
	ID        uint  `gorm:"primaryKey"`
	ChatID    int64 `gorm:"uniqueIndex"`
	ChatTitle string
	AddedBy   int64
	CreatedAt time.Time
}

type ChatApprovalRequest struct {
	ID        uint  `gorm:"primaryKey"`
	ChatID    int64 `gorm:"index"`
	ChatTitle string
	UserID    int64
	Username  string
	Status    string `gorm:"default:'pending'"`
	CreatedAt time.Time
}

type Bot struct {
	telebot *telebot.Bot
	db      *gorm.DB
	openai  *openai.Client
	config  *Config
}

func loadConfig() *Config {
	maxTokens := 1200
	if tokensStr := getEnv("OPENAI_MAX_TOKENS", ""); tokensStr != "" {
		if parsed, err := strconv.Atoi(tokensStr); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	return &Config{
		TelegramToken:   getEnv("TELEGRAM_BOT_TOKEN", ""),
		OpenAIAPIKey:    getEnv("OPENAI_API_KEY", ""),
		OpenAIBaseURL:   getEnv("OPENAI_BASE_URL", "http://31.172.78.152:9000/v1"),
		DatabasePath:    getEnv("DATABASE_PATH", "./summarybot.db"),
		Port:            getEnv("PORT", "8080"),
		BotUsername:     getEnv("BOT_USERNAME", "zagichak_bot"),
		AllowedChats:    parseInt64List(getEnv("ALLOWED_CHATS", "")),
		AdminUserIDs:    parseInt64List(getEnv("ADMIN_USER_IDS", "")),
		RequireApproval: getEnv("REQUIRE_APPROVAL", "true") == "true",
		OpenAIModel:     getEnv("OPENAI_MODEL", "gpt-4o-mini"),
		MaxTokens:       maxTokens,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func escapeHTML(text string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(text)
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

func initDatabase(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	err = db.AutoMigrate(&Message{}, &ChatSummary{}, &AllowedChat{}, &ChatApprovalRequest{})
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

func (b *Bot) isChatAllowed(chatID int64) bool {
	if chatID > 0 {
		return true
	}

	for _, allowedID := range b.config.AllowedChats {
		if allowedID == chatID {
			return true
		}
	}

	var count int64
	b.db.Model(&AllowedChat{}).Where("chat_id = ?", chatID).Count(&count)
	return count > 0
}

func (b *Bot) isAdmin(userID int64) bool {
	for _, adminID := range b.config.AdminUserIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}

func (b *Bot) requestChatApproval(chatID int64, chatTitle string, userID int64, username string) {
	var existingRequest ChatApprovalRequest
	result := b.db.Where("chat_id = ? AND status = 'pending'", chatID).First(&existingRequest)
	if result.Error == nil {
		return
	}

	request := ChatApprovalRequest{
		ChatID:    chatID,
		ChatTitle: chatTitle,
		UserID:    userID,
		Username:  username,
		Status:    "pending",
	}

	b.db.Create(&request)

	b.notifyAdminsAboutNewRequest(request)
}

func (b *Bot) notifyAdminsAboutNewRequest(request ChatApprovalRequest) {
	if len(b.config.AdminUserIDs) == 0 {
		return
	}

	message := fmt.Sprintf("🔐 <b>Новый запрос доступа</b>\n\n"+
		"<b>Чат:</b> %s (%d)\n"+
		"<b>Пользователь:</b> @%s (%d)\n\n"+
		"Используйте команды:\n"+
		"• <code>/approve %d</code> - разрешить\n"+
		"• <code>/reject %d</code> - отклонить\n"+
		"• <code>/pending</code> - показать все запросы",
		escapeHTML(request.ChatTitle), request.ChatID,
		escapeHTML(request.Username), request.UserID,
		request.ChatID, request.ChatID)

	for _, adminID := range b.config.AdminUserIDs {
		chat := &telebot.Chat{ID: adminID}
		b.telebot.Send(chat, message, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}
}

func (b *Bot) saveMessage(m *telebot.Message) {
	if m.Text == "" {
		return
	}

	if !b.isChatAllowed(m.Chat.ID) {
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

	prompt := fmt.Sprintf(`Проанализируй сообщения из Telegram чата за %s и создай детальное резюме самых интересных моментов на русском языке. 

Сообщения:
%s

Требования к резюме:
- Выдели 5-8 самых интересных/важных тем, событий или дискуссий
- Используй подходящие эмодзи для каждой темы
- Для каждой темы дай 2-3 предложения описания
- Сохраняй важные детали и контекст
- Если есть ссылки, упоминания людей или важные решения - обязательно включи
- Пиши в живом, неформальном стиле как для друзей
- Если было мало активности или ничего интересного - честно об этом скажи
- Группируй связанные сообщения по темам
- Используй HTML теги для выделения: <b>жирный</b>, <i>курсив</i>
- Для заголовков используй <b>текст</b>

Формат ответа: 

🔥 <b>Горячие темы:</b>
• [тема 1 с эмодзи] - описание
• [тема 2 с эмодзи] - описание
...

💬 <b>Интересные моменты:</b>
• [момент 1] 
• [момент 2]
...

🔗 <b>Важные ссылки/решения:</b> (если есть)
• [ссылка/решение]

Только основной текст резюме, без дополнительных пояснений.`,
		period, textBuilder.String(), period)

	resp, err := b.openai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: b.config.OpenAIModel,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   b.config.MaxTokens,
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

	if !b.isChatAllowed(c.Chat().ID) {
		if b.config.RequireApproval && c.Chat().ID < 0 {
			chatTitle := c.Chat().Title
			if chatTitle == "" {
				chatTitle = "Неизвестный чат"
			}

			b.requestChatApproval(c.Chat().ID, chatTitle, c.Sender().ID, c.Sender().Username)

			return c.Reply("❌ Доступ к этому чату не разрешен.\n\n" +
				"📝 Запрос на одобрение отправлен администраторам.\n" +
				"⏳ Ожидайте подтверждения доступа.")
		}

		return c.Reply("❌ У меня нет доступа к этому чату.")
	}

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

	c.Bot().Delete(statusMsg)

	summaryText := fmt.Sprintf("📋 <b>Резюме за %s</b>\n\n%s\n\n<i>Проанализировано сообщений: %d</i>",
		period, summary, len(messages))

	return c.Reply(summaryText, &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

func (b *Bot) handleStart(c telebot.Context) error {
	if c.Chat().ID < 0 && !b.isChatAllowed(c.Chat().ID) {
		if b.config.RequireApproval {
			chatTitle := c.Chat().Title
			if chatTitle == "" {
				chatTitle = "Неизвестный чат"
			}

			b.requestChatApproval(c.Chat().ID, chatTitle, c.Sender().ID, c.Sender().Username)

			return c.Reply("❌ Доступ к этому чату не разрешен.\n\n" +
				"📝 Запрос на одобрение отправлен администраторам.\n" +
				"⏳ Ожидайте подтверждения доступа.")
		}

		return c.Reply("❌ У меня нет доступа к этому чату.")
	}

	welcomeText := `Привет! 👋 

Я бот для создания резюме чата. Просто упомяни меня и скажи:
• @zagichak_bot что было за сегодня
• @zagichak_bot что было за вчера  
• @zagichak_bot что было за позавчера
• @zagichak_bot что было за 3 дня

Я проанализирую сообщения и выдам самое интересное! 🤖✨`

	return c.Reply(welcomeText)
}

func (b *Bot) handleApprove(c telebot.Context) error {
	if !b.isAdmin(c.Sender().ID) {
		return c.Reply("❌ У вас нет прав администратора.")
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		return c.Reply("📝 Использование: <code>/approve &lt;chat_id&gt;</code>", &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}

	chatID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return c.Reply("❌ Неверный формат chat_id")
	}

	result := b.db.Model(&ChatApprovalRequest{}).
		Where("chat_id = ? AND status = 'pending'", chatID).
		Update("status", "approved")

	if result.RowsAffected == 0 {
		return c.Reply("❌ Запрос не найден или уже обработан")
	}

	allowedChat := AllowedChat{
		ChatID:  chatID,
		AddedBy: c.Sender().ID,
	}

	var request ChatApprovalRequest
	if b.db.Where("chat_id = ?", chatID).First(&request).Error == nil {
		allowedChat.ChatTitle = request.ChatTitle
	}

	b.db.Create(&allowedChat)

	return c.Reply(fmt.Sprintf("✅ Чат %d одобрен и добавлен в разрешенные!", chatID))
}

func (b *Bot) handleReject(c telebot.Context) error {
	if !b.isAdmin(c.Sender().ID) {
		return c.Reply("❌ У вас нет прав администратора.")
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		return c.Reply("📝 Использование: <code>/reject &lt;chat_id&gt;</code>", &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}

	chatID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return c.Reply("❌ Неверный формат chat_id")
	}

	result := b.db.Model(&ChatApprovalRequest{}).
		Where("chat_id = ? AND status = 'pending'", chatID).
		Update("status", "rejected")

	if result.RowsAffected == 0 {
		return c.Reply("❌ Запрос не найден или уже обработан")
	}

	return c.Reply(fmt.Sprintf("🚫 Запрос для чата %d отклонен.", chatID))
}

func (b *Bot) handlePending(c telebot.Context) error {
	if !b.isAdmin(c.Sender().ID) {
		return c.Reply("❌ У вас нет прав администратора.")
	}

	var requests []ChatApprovalRequest
	b.db.Where("status = 'pending'").Order("created_at DESC").Find(&requests)

	if len(requests) == 0 {
		return c.Reply("📭 Нет ожидающих запросов.")
	}

	var response strings.Builder
	response.WriteString("📋 <b>Ожидающие запросы:</b>\n\n")

	for _, req := range requests {
		response.WriteString(fmt.Sprintf("🔹 <b>%s</b> (%d)\n", escapeHTML(req.ChatTitle), req.ChatID))
		response.WriteString(fmt.Sprintf("   👤 @%s (%d)\n", escapeHTML(req.Username), req.UserID))
		response.WriteString(fmt.Sprintf("   📅 %s\n", req.CreatedAt.Format("02.01.2006 15:04")))
		response.WriteString(fmt.Sprintf("   • <code>/approve %d</code> <code>/reject %d</code>\n\n", req.ChatID, req.ChatID))
	}

	return c.Reply(response.String(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

func (b *Bot) handleAllowedChats(c telebot.Context) error {
	if !b.isAdmin(c.Sender().ID) {
		return c.Reply("❌ У вас нет прав администратора.")
	}

	var chats []AllowedChat
	b.db.Order("created_at DESC").Find(&chats)

	var response strings.Builder
	response.WriteString("📋 <b>Разрешенные чаты:</b>\n\n")

	for _, chatID := range b.config.AllowedChats {
		response.WriteString(fmt.Sprintf("🔹 %d <i>(из конфига)</i>\n", chatID))
	}

	for _, chat := range chats {
		response.WriteString(fmt.Sprintf("🔹 <b>%s</b> (%d)\n", escapeHTML(chat.ChatTitle), chat.ChatID))
		response.WriteString(fmt.Sprintf("   📅 %s\n\n", chat.CreatedAt.Format("02.01.2006 15:04")))
	}

	if len(chats) == 0 && len(b.config.AllowedChats) == 0 {
		response.WriteString("📭 Нет разрешенных чатов.")
	}

	return c.Reply(response.String(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
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
	tgBot.Handle("/approve", bot.handleApprove)
	tgBot.Handle("/reject", bot.handleReject)
	tgBot.Handle("/pending", bot.handlePending)
	tgBot.Handle("/allowed", bot.handleAllowedChats)
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
