package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
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

type SwearStats struct {
	ID        uint  `gorm:"primaryKey"`
	ChatID    int64 `gorm:"index"`
	UserID    int64 `gorm:"index"`
	Username  string
	SwearWord string
	Count     int `gorm:"default:1"`
	UpdatedAt time.Time
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

	err = db.AutoMigrate(&Message{}, &ChatSummary{}, &AllowedChat{}, &ChatApprovalRequest{}, &SwearStats{})
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
	for _, allowedID := range b.config.AllowedChats {
		if allowedID == chatID {
			log.Printf("Чат %d разрешен (найден в конфиге)", chatID)
			return true
		}
	}

	var count int64
	result := b.db.Model(&AllowedChat{}).Where("chat_id = ?", chatID).Count(&count)
	if result.Error != nil {
		log.Printf("Ошибка проверки чата %d в БД: %v", chatID, result.Error)
		return false
	}

	allowed := count > 0
	if allowed {
		log.Printf("Чат %d разрешен (найден в БД)", chatID)
	} else {
		log.Printf("Чат %d НЕ разрешен (не найден ни в конфиге, ни в БД)", chatID)
	}

	return allowed
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
		log.Printf("Пропускаем сообщение без текста от %s в чате %d", m.Sender.Username, m.Chat.ID)
		return
	}

	if !b.isChatAllowed(m.Chat.ID) {
		log.Printf("Чат %d не разрешен, сообщение не сохраняется", m.Chat.ID)
		return
	}

	message := Message{
		ChatID:    m.Chat.ID,
		UserID:    m.Sender.ID,
		Username:  m.Sender.Username,
		Text:      m.Text,
		Timestamp: time.Unix(m.Unixtime, 0),
	}

	result := b.db.Create(&message)
	if result.Error != nil {
		log.Printf("Ошибка сохранения сообщения в БД: %v", result.Error)
	} else {
		log.Printf("Сообщение сохранено: чат %d, пользователь %s, ID записи: %d",
			m.Chat.ID, m.Sender.Username, message.ID)
	}

	b.checkAndSaveSwearStats(m)
}

func (b *Bot) checkAndSaveSwearStats(m *telebot.Message) {
	if m.Chat.ID > 0 {
		return
	}

	swearWords := []string{
		"блять", "хуй", "пизда", "ебать", "сука", "говно", "дерьмо",
		"мудак", "долбоеб", "ублюдок", "сволочь", "падла", "гавно",
		"хрен", "херня", "охуеть", "заебать", "проебать", "наебать",
	}

	text := strings.ToLower(m.Text)
	for _, swear := range swearWords {
		if strings.Contains(text, swear) {
			var stat SwearStats
			result := b.db.Where("chat_id = ? AND user_id = ? AND swear_word = ?",
				m.Chat.ID, m.Sender.ID, swear).First(&stat)

			if result.Error == nil {
				// Обновляем существующую запись
				b.db.Model(&stat).Updates(SwearStats{
					Count:     stat.Count + 1,
					UpdatedAt: time.Now(),
				})
			} else {
				newStat := SwearStats{
					ChatID:    m.Chat.ID,
					UserID:    m.Sender.ID,
					Username:  m.Sender.Username,
					SwearWord: swear,
					Count:     1,
					UpdatedAt: time.Now(),
				}
				b.db.Create(&newStat)
			}
		}
	}
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
		return fmt.Sprintf("За %s никто ничего не писал, братан 🤷‍♂️", period), nil
	}

	if len(messages) < b.config.MinMessagesForAI {
		log.Printf("Мало сообщений для AI анализа: %d < %d (порог)", len(messages), b.config.MinMessagesForAI)
		return fmt.Sprintf("За %s было всего %d сообщений - слишком мало для нормального резюме, братан 📱\n\nПопробуй запросить резюме когда народ побольше пообщается! (нужно минимум %d сообщений)",
			period, len(messages), b.config.MinMessagesForAI), nil
	}

	log.Printf("Отправляем %d сообщений в OpenAI для анализа", len(messages))

	var textBuilder strings.Builder
	for _, msg := range messages {
		textBuilder.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			msg.Timestamp.Format("15:04"), msg.Username, msg.Text))
	}

	systemPrompt := `Ты крутой пацан с района, который умеет анализировать чатики и делать огненные резюме для корешей. 

ВАЖНО - АНАЛИЗИРУЙ ТОЛЬКО РЕАЛЬНЫЕ СООБЩЕНИЯ:
- Пересказывай ТОЛЬКО то, что реально было написано в чате
- НЕ выдумывай события, имена, темы которых не было
- Если сообщений мало или они скучные - честно говори об этом
- Точно передавай факты, но своими словами в классном стиле

Твой стиль:
- Говоришь как настоящий братан - простым языком, с прикольными фразочками
- Используешь сленг: "братан", "чел", "тема", "движ", "кайф", "жесть" и т.д.
- Эмодзи ставишь к месту, но не переборщиваешь
- Пишешь живо и интересно, как будто рассказываешь корешу что было
- Если что-то скучное - честно говоришь об этом

Что ты делаешь:
- Выделяешь 3-6 самых интересных тем/событий ИЗ РЕАЛЬНЫХ СООБЩЕНИЙ
- Группируешь связанные сообщения по темам
- Сохраняешь важные детали: ссылки, упоминания, решения
- Используешь HTML теги: <b>жирный</b>, <i>курсив</i>
- Пишешь 1-2 предложения на тему, коротко и по делу

Формат ответа:

🔥 <b>Что было жарко:</b>
• [тема с эмодзи] - краткое описание ТОЛЬКО из реальных сообщений

💬 <b>Интересные движи:</b>
• [движ 1 из реальных сообщений]
• [движ 2 из реальных сообщений]

🔗 <b>Полезняк:</b> (только если есть ссылки/решения)
• [ссылка или решение]

Главное - пиши как пацан для пацанов, но строго по фактам из чата!`

	userPrompt := fmt.Sprintf(`Проанализируй ВСЕ сообщения ниже и сделай резюме за %s. 

ВАЖНО: Анализируй ТОЛЬКО эти сообщения, не выдумывай ничего лишнего!

Всего сообщений для анализа: %d

Сообщения:
%s`, period, len(messages), textBuilder.String())

	resp, err := b.openai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: b.config.OpenAIModel,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: systemPrompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: userPrompt,
				},
			},
			MaxTokens:   b.config.MaxTokens,
			Temperature: 0.3,
		},
	)

	if err != nil {
		return "", fmt.Errorf("ошибка OpenAI API: %v", err)
	}

	if len(resp.Choices) == 0 {
		return "Не смог замутить резюме, братан 😞", nil
	}

	return resp.Choices[0].Message.Content, nil
}

func (b *Bot) handleSummaryRequest(c telebot.Context) error {
	message := c.Message()

	if c.Chat().ID > 0 {
		return c.Reply("❌ Summary доступен только в групповых чатах, братан! 🤖")
	}

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

	log.Printf("Обрабатываю запрос резюме для чата %d на период: %s (дней назад: %d)", c.Chat().ID, period, days)

	statusMsg, _ := c.Bot().Send(c.Chat(), "Генерирую резюме... ⏳")

	messages, err := b.getMessagesForPeriod(c.Chat().ID, days)
	if err != nil {
		log.Printf("Ошибка получения сообщений для чата %d: %v", c.Chat().ID, err)
		c.Bot().Delete(statusMsg)
		return c.Reply("Ошибка при получении сообщений 😞")
	}

	log.Printf("Найдено сообщений для резюме: %d", len(messages))

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
	if c.Chat().ID > 0 {
		if b.isAdmin(c.Sender().ID) {
			welcomeText := `Привет, админ! 👑

Доступные команды:
• /approve <chat_id> - одобрить чат
• /reject <chat_id> - отклонить запрос
• /pending - показать ожидающие запросы
• /allowed - список разрешенных чатов

В групповых чатах также доступны:
• /roast_random - жесткий подкол случайному корешу 🔥
• /reminder_random - "важное" напоминание кому-то 😏

Summary доступен только в групповых чатах! 🤖`
			return c.Reply(welcomeText)
		} else {
			return c.Reply("👋 Привет! Этот бот работает только в групповых чатах. Добавь меня в группу и попроси резюме!")
		}
	}

	if !b.isChatAllowed(c.Chat().ID) {
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

Также попробуй:
• /roast_random - жесткий подкол случайному корешу 🔥
• /reminder_random - "важное" напоминание кому-то 😏
• /top_mat - топ матершинников чата 🤬

Я проанализирую сообщения и выдам самое интересное! 🤖✨`

	return c.Reply(welcomeText)
}

func (b *Bot) handleUserJoined(c telebot.Context) error {
	// Работает только в групповых чатах
	if c.Chat().ID > 0 || !b.isChatAllowed(c.Chat().ID) {
		return nil
	}

	for _, user := range c.Message().UsersJoined {
		if user.IsBot {
			continue // Пропускаем ботов
		}

		username := user.Username
		if username == "" {
			username = user.FirstName
		}

		greetings := []string{
			"О, привет %s! 👋 Хуй сосал? Расскажи о себе, не стесняйся! 😏",
			"Смотрите кто к нам заглянул! 👀 %s, надеюсь не из полиции? 🚔",
			"Ебааа, %s в здании! 🎉 Сразу видно - человек с хорошим вкусом 😎",
			"%s подтянулся! 💪 Братан, тут весело, оставайся! 🔥",
			"О боже, %s! 😱 Ты случайно не тот самый легендарный парень? 🌟",
			"Здарова %s! 🤘 Мамке не говори что тут сидишь, ладно? 🤫",
			"Вау, %s! 🎪 Цирк потерял клоуна или ты просто в гости? 🤡",
			"%s на связи! 📡 Надеюсь у тебя крепкие нервы, тут отрываемся по полной! 🎭",
			"Глянь-ка, %s объявился! 👁️ Сразу видно - интеллигент блядь! 🎩",
			"Эй %s! 🗣️ Водка есть? Нет? Ну тогда просто посиди, пообщайся! 🍻",
			"О май гад, %s! 😲 Ты специально к нам или GPS обосрался? 🗺️",
			"%s в чате! 🎊 Давай знакомиться, расскажи что по жизни делаешь! 💼",
			"Вот это да, %s! 🎯 Точно не перепутал чат? Мы тут дичь творим! 🦌",
			"Добро пожаловать %s! 🏠 Тапки снял? Проходи, располагайся! 👟",
			"Ого, %s подъехал! 🚗 Бензин кончился или просто скучно стало? ⛽",
		}

		randomIndex := rand.Intn(len(greetings))
		greeting := fmt.Sprintf(greetings[randomIndex], escapeHTML(username))

		c.Reply(greeting, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}

	return nil
}

func (b *Bot) generateRoastForUser(username string, chatID int64) (string, error) {
	systemPrompt := `Ты злобный пацан с района, который делает максимально жесткие, но дружеские подколы. 

Твоя задача - сделать ЖЕСТКИЙ, но не переходящий границы подкол конкретному человеку в дружеском чате.

ВАЖНО:
- Подкол должен быть МАКСИМАЛЬНО ЖЕСТКИМ, но не оскорбительным
- Это дружеский чат, все свои - можно себе позволить больше
- Используй креативные, остроумные подъебки
- Никаких серьезных оскорблений, только веселая жесть
- Используй эмодзи, сленг, юмор
- Длина: 1-2 предложения максимум
- Можешь пошутить над внешностью, поведением, привычками (в рамках дружеского троллинга)

Стиль:
- Говори как пацан с улицы
- Используй слова: "братан", "чел", "кореш", "лох", "жесть" и т.д.
- Можно слегка матерный юмор в рамках приличия
- Острый, саркастичный, но дружелюбный тон

Формат ответа: просто жесткий подкол без лишних слов.`

	userPrompt := fmt.Sprintf(`Сделай максимально жесткий, но дружеский подкол пользователю с ником "%s". 
Это дружеский чат, все корешы, можно жестко тролить!`, username)

	resp, err := b.openai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: b.config.OpenAIModel,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: systemPrompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: userPrompt,
				},
			},
			MaxTokens:   200,
			Temperature: 0.8,
		},
	)

	if err != nil {
		return "", fmt.Errorf("ошибка OpenAI API: %v", err)
	}

	if len(resp.Choices) == 0 {
		return "Даже я не знаю как тебя подколоть, братан 😂", nil
	}

	return resp.Choices[0].Message.Content, nil
}

func (b *Bot) generateRandomReminder(username string) (string, error) {
	systemPrompt := `Ты заботливый, но жесткий кореш, который "напоминает" людям о разной фигне.

Твоя задача - придумать смешное "напоминание" которое на самом деле просто жесткий прикол.

ВАЖНО:
- Это НЕ реальное напоминание, а просто повод подколоть человека
- Выдумывай абсурдные, смешные "обязанности" и "дела"
- Будь максимально креативным и жестким
- Используй дружеский, но наглый тон
- Можно упоминать: работу, быт, отношения, хобби, привычки
- Длина: 1-2 предложения

Примеры стиля:
"Эй {username}, ты забыл покормить свою депрессию!"
"Напоминаю {username}: пора менять носки, соседи жалуются!"
"Кореш {username}, твоя очередь выносить мусор из головы!"

Стиль:
- Говори как пацан
- Используй слова: "братан", "кореш", "чел" и т.д.
- Жесткий юмор в рамках дружбы
- Абсурдные "напоминания"

Формат: "Эй [username], [жесткое напоминание-прикол]"`

	userPrompt := fmt.Sprintf(`Придумай жесткое "напоминание"-прикол для пользователя "%s". 
Это должно быть смешно и абсурдно!`, username)

	resp, err := b.openai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: b.config.OpenAIModel,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: systemPrompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: userPrompt,
				},
			},
			MaxTokens:   150,
			Temperature: 0.9,
		},
	)

	if err != nil {
		return "", fmt.Errorf("ошибка OpenAI API: %v", err)
	}

	if len(resp.Choices) == 0 {
		fallbackReminders := []string{
			"Эй %s, ты забыл покормить свою лень! 😴",
			"Напоминаю %s: пора менять носки, даже я чувствую! 🧦",
			"Кореш %s, твоя очередь выносить мусор из головы! 🗑️",
			"Братан %s, ты обещал стать человеком, когда уже? 🤔",
			"Эй %s, мамка просила передать - убери в комнате! 🏠",
		}
		randomIndex := rand.Intn(len(fallbackReminders))
		return fmt.Sprintf(fallbackReminders[randomIndex], username), nil
	}

	return resp.Choices[0].Message.Content, nil
}

func (b *Bot) getRandomActiveUser(chatID int64) (string, int64, error) {
	var users []struct {
		Username string
		UserID   int64
		Count    int64
	}

	sevenDaysAgo := time.Now().AddDate(0, 0, -7)

	err := b.db.Raw(`
		SELECT username, user_id, COUNT(*) as count 
		FROM messages 
		WHERE chat_id = ? AND timestamp >= ? AND username != '' 
		GROUP BY user_id, username 
		HAVING count >= 3
		ORDER BY count DESC 
		LIMIT 20
	`, chatID, sevenDaysAgo).Scan(&users)

	if err != nil || len(users) == 0 {
		return "", 0, fmt.Errorf("нет активных пользователей")
	}

	randomIndex := rand.Intn(len(users))
	selectedUser := users[randomIndex]

	return selectedUser.Username, selectedUser.UserID, nil
}

func (b *Bot) handleRoastUser(c telebot.Context) error {
	if c.Chat().ID > 0 || !b.isChatAllowed(c.Chat().ID) {
		return c.Reply("❌ Подколы только в групповых чатах!")
	}

	username, _, err := b.getRandomActiveUser(c.Chat().ID)
	if err != nil {
		return c.Reply("😔 Не могу найти кого подколоть - все молчат!")
	}

	roast, err := b.generateRoastForUser(username, c.Chat().ID)
	if err != nil {
		log.Printf("Ошибка генерации подкола: %v", err)
		return c.Reply("Сломался генератор подколов 🤖💥")
	}

	return c.Reply("🔥 <b>Случайный подкол:</b>\n\n"+roast, &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

func (b *Bot) handleReminder(c telebot.Context) error {
	if c.Chat().ID > 0 || !b.isChatAllowed(c.Chat().ID) {
		return c.Reply("❌ Напоминания только в групповых чатах!")
	}

	username, _, err := b.getRandomActiveUser(c.Chat().ID)
	if err != nil {
		return c.Reply("😔 Некому напоминать - все исчезли!")
	}

	reminder, err := b.generateRandomReminder(username)
	if err != nil {
		log.Printf("Ошибка генерации напоминания: %v", err)
		return c.Reply("Забыл что хотел напомнить 🤪")
	}

	return c.Reply("⏰ <b>Важное напоминание:</b>\n\n"+reminder, &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

func (b *Bot) maybeDoRandomAction(c telebot.Context) {
	if c.Chat().ID > 0 || !b.isChatAllowed(c.Chat().ID) {
		return
	}

	if rand.Intn(100) != 0 {
		return
	}

	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	var userCount int64
	b.db.Raw(`
		SELECT COUNT(DISTINCT user_id) 
		FROM messages 
		WHERE chat_id = ? AND timestamp >= ?
	`, c.Chat().ID, sevenDaysAgo).Scan(&userCount)

	if userCount < 3 {
		return
	}

	actionType := rand.Intn(2)

	username, _, err := b.getRandomActiveUser(c.Chat().ID)
	if err != nil {
		return
	}

	if actionType == 0 {
		roast, err := b.generateRoastForUser(username, c.Chat().ID)
		if err != nil {
			return
		}

		message := "🎯 <b>Внезапный подкол:</b>\n\n" + roast
		c.Bot().Send(c.Chat(), message, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})

		log.Printf("Автоматический подкол для %s в чате %d", username, c.Chat().ID)
	} else {
		reminder, err := b.generateRandomReminder(username)
		if err != nil {
			return
		}

		message := "🔔 <b>Срочное напоминание:</b>\n\n" + reminder
		c.Bot().Send(c.Chat(), message, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})

		log.Printf("Автоматическое напоминание для %s в чате %d", username, c.Chat().ID)
	}
}
func (b *Bot) handleTopMat(c telebot.Context) error {
	if c.Chat().ID > 0 || !b.isChatAllowed(c.Chat().ID) {
		return c.Reply("❌ Статистика мата только в групповых чатах!")
	}

	var stats []struct {
		Username string
		Total    int
	}

	b.db.Raw(`
		SELECT username, SUM(count) as total 
		FROM swear_stats 
		WHERE chat_id = ? 
		GROUP BY user_id, username 
		ORDER BY total DESC 
		LIMIT 10
	`, c.Chat().ID).Scan(&stats)

	if len(stats) == 0 {
		return c.Reply("🤯 Невероятно! В этом чате еще никто не матерился! 😇\n\nИли я просто еще не успел все посчитать... 🤔")
	}

	var response strings.Builder
	response.WriteString("🤬 <b>Топ матершинников чата:</b>\n\n")

	medals := []string{"🥇", "🥈", "🥉"}
	for i, stat := range stats {
		var medal string
		if i < 3 {
			medal = medals[i]
		} else {
			medal = fmt.Sprintf("%d.", i+1)
		}

		response.WriteString(fmt.Sprintf("%s <b>%s</b> - %d раз\n",
			medal, escapeHTML(stat.Username), stat.Total))
	}

	response.WriteString("\n<i>Статистика ведется с момента последнего обновления бота 📊</i>")

	return c.Reply(response.String(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

func (b *Bot) handleRoast(c telebot.Context) error {
	text := strings.ToLower(c.Message().Text)

	var roastResponses []string

	if strings.Contains(text, "сосал") || strings.Contains(text, "соси") {
		roastResponses = []string{
			"Ой, какой смешной 😂 Иди лучше мамке помоги посуду помыть",
			"Вау, какая оригинальность! 🥱 Года в 2005 может и засмеялись бы",
			"Серьезно? Это лучшее что ты смог придумать? 😂 Слабовато, чел",
			"Какой же ты клоун 🤪 Ладно, развеселил немного",
			"Ты серьезно думал что это смешно? 💀 Лучше б молчал, братан",
			"Ого, какой острослов! 🎭 Наверное в школе класс-комиком был",
			"Мамочка, я умею плохие слова! 👶 Папе расскажешь?",
			"Какая фантазия бурная! 🌪️ Лучше б на что-то полезное направил",
			"Блин, аж в детство вернулся 🏫 Такие же шутки в 5-м классе были",
			"Вижу, интеллектом природа не наградила 🧠 Зато смелости хватает",
			"А потом удивляемся почему друзей нет 🤷‍♂️",
			"Красиво излагаешь мысли! 💭 Поэт, блин",
			"Мне кажется или ты путаешь меня с зеркалом? 🪞",
			"Какой ты забавный! 🎪 Цирк тебя потерял?",
			"Ну и словарный запас! 📚 Мама гордится?",
			"Я так понимаю, сегодня день дурака? 🤡 А то календарь врет",
			"Бро, тебе точно больше 10 лет? 👶 А то сомнения закрадываются",
			"Ничего, вырастешь - поумнеешь 📈 Хотя не факт",
			"Оригинально! 🎨 Небось в универе на филфаке учишься?",
			"А давай лучше про погоду поговорим? ☀️ Или это тоже сложно?",
		}
	} else if strings.Contains(text, "дурак") || strings.Contains(text, "идиот") || strings.Contains(text, "тупой") {
		roastResponses = []string{
			"Зеркало дома сломалось? 🪞 Может починишь сначала его",
			"Кто бы говорил 🙄 Сначала на себя посмотри",
			"Проекция называется 📽️ Изучи этот термин",
			"Самокритично! Но я-то тут причем? 😏",
			"Диагноз себе ставишь? Молодец, самосознание есть 🤓",
			"Автопортрет рисуешь? 🎨 Получается похоже!",
			"Говоришь о том, что знаешь лучше всего? 🎯 Респект за честность",
			"А ты хорошо себя знаешь! 🤝 Самоанализ - полезная штука",
			"Экспертное мнение! 👨‍🎓 Видимо, в этом вопросе ты профи",
			"Интересно... а откуда такие познания? 🤔 Личный опыт?",
			"Рассказываешь о себе? 📖 Автобиография получается",
			"Какая самокритика! 👏 Но причем тут я?",
			"Спасибо за характеристику! 📝 Только адресом ошибся",
			"Ого, какая самооценка! 💯 Честный ты парень",
			"Делишься опытом? 🎓 Ценим твою открытость",
			"А ты хорошо разбираешься в этом вопросе! 🧠 Откуда знания?",
			"Видимо, говоришь из личного опыта 💼 Уважаю экспертов",
			"Интересная самопрезентация! 🎪 Креативно подходишь к знакомству",
			"Ты про себя или всё-таки обо мне? 🤷‍♂️ А то я запутался",
			"Какой ты откровенный! 💫 Редко встретишь такую честность",
			"Ну и самооценочка! 📊 Психолог бы заинтересовался",
		}
	} else if strings.Contains(text, "гей") || strings.Contains(text, "пидор") {
		roastResponses = []string{
			"2007 год на связи 📞 Хочет свои шутки обратно",
			"Какой прогрессивный юмор! 🏳️‍🌈 Дедушка бы гордился",
			"Ой, боюсь-боюсь 😱 Иди лучше друзей найди",
			"Тебе лет сколько, 12? 👶 Подрасти сначала",
			"Времена меняются, а ты нет 🦕 Эволюция мимо прошла?",
			"Вау, какая архаика! 🏺 Музей тебя потерял?",
			"Совсем фантазия иссякла? 🎭 Хоть бы что-то новое придумал",
			"А ты точно в курсе, какой сейчас год? 📅 Время идет, знаешь ли",
			"Думаешь, это кого-то задевает? 😂 Мило, честное слово",
			"Какой ты консервативный! 🏛️ Дедушкины ценности храним?",
			"Ого, прямо 90-е вернулись! 📼 Ностальгия по детству?",
			"А что, по-другому обидеть не получается? 🤔 Словарный запас кончился?",
			"Боишься чего-то? 😰 А то так агрессивно реагируешь",
			"Интересные у тебя комплексы 🧠 С психологом не пробовал поговорить?",
			"Такое ощущение, что ты в 2000-х застрял ⏰ Путешествие во времени?",
			"А ты точно не перепутал века? 🕰️ Сейчас всё-таки 21-й",
			"Папа так учил? 👨 Или сам до такого дошел?",
			"Какие стереотипы! 📚 В интернете начитался?",
			"А давай без ярлыков? 🏷️ Или думать сложно?",
			"Мне кажется, ты что-то компенсируешь 🤷‍♂️ Все в порядке дома?",
		}
	} else {
		roastResponses = []string{
			"Юморист нашелся 🤡 В детском саду таких шуток не было даже",
			"Комик доморощенный 😴 Иди лучше что-то полезное делай",
			"Хахаха, очень смешно... НЕТ 🙄 Попробуй еще раз, может получится",
			"О, у нас тут комедиант! 🎭 Только шутки твои как анекдоты от дедушки",
			"Ты случайно не из КВН сбежал? 😏 Такой же уровень юмора",
			"Какая оригинальность! 🎨 Небось всю ночь придумывал",
			"Мам, смотри, я умею матюкаться! 👶 Вырастешь - поймешь как глупо это выглядит",
			"Ого, какая креативность! 🌟 Нобелевскую премию дают за такое?",
			"Блестящий интеллект! ✨ Наверное в школе медаль за остроумие получал",
			"Ух ты, какой острый язычок! 👅 Родители гордятся воспитанием?",
			"Видимо сегодня твой день! 📅 Или каждый день такой успешный?",
			"А ты точно не профессиональный обидчик? 💼 Такие навыки редко встретишь",
			"Интересный подход к общению! 🤝 В универе на такую специальность учат?",
			"Какой ты храбрый в интернете! 💪 А в реале так же смел?",
			"Ничего, бывает 🤷‍♂️ У всех плохие дни случаются",
			"А давай лучше о чем-то приятном? ☀️ Или только ругаться умеешь?",
			"Такое ощущение, что тебя кто-то обидел 😢 Хочешь поговорить?",
			"Видимо настроение не очень 😔 Может чаю попьешь?",
			"А ты знаешь, что есть более конструктивные способы общения? 🗣️",
			"Интересная стратегия знакомства! 🎯 Работает?",
			"Такой талант пропадает! 🎪 В цирк не пробовал устроиться?",
			"Ого, какая энергия! ⚡ Лучше б в спорт направил",
			"А мне кажется, ты хороший парень 😊 Просто день неудачный",
			"Давай без агрессии? 🕊️ Жизнь и так сложная",
			"Может просто поздоровался бы? 👋 Воспитание никто не отменял",
		}
	}

	randomIndex := rand.Intn(len(roastResponses))
	response := roastResponses[randomIndex]

	log.Printf("Отвечаем умнику %s в чате %d", c.Sender().Username, c.Chat().ID)

	return c.Reply(response)
}

func (b *Bot) isRoastMessage(text string) bool {
	cleanText := strings.ToLower(text)
	cleanText = strings.ReplaceAll(cleanText, "@"+strings.ToLower(b.config.BotUsername), "")
	cleanText = strings.TrimSpace(cleanText)

	roastTriggers := []string{
		"сосал", "сосешь", "соси", "пидор", "гей", "лох",
		"дурак", "идиот", "тупой", "долбоеб", "мудак", "ебан",
		"дебил", "придурок", "кретин", "козел", "свинья", "урод",
		"падла", "говно", "хуй", "пизда", "ебать", "блять",
		"сука", "шлюха", "обоссался", "обосрался", "ублюдок",
	}

	for _, trigger := range roastTriggers {
		if strings.Contains(cleanText, trigger) {
			log.Printf("Найден триггер '%s' в сообщении: %s", trigger, cleanText)
			return true
		}
	}

	if len(cleanText) <= 15 && (strings.Contains(cleanText, "?") || strings.Contains(cleanText, "???")) {
		provocativePatterns := []string{
			"как дела", "че как", "живой", "работаешь", "спишь", "ку", "привет",
		}

		for _, pattern := range provocativePatterns {
			if strings.Contains(cleanText, pattern) {
				return false
			}
		}

		if strings.Count(cleanText, "?") >= 1 && len(strings.TrimSpace(strings.ReplaceAll(cleanText, "?", ""))) <= 8 {
			log.Printf("Подозрительное короткое сообщение с вопросами: %s", cleanText)
			return true
		}
	}

	return false
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
	rand.Seed(time.Now().UnixNano())

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
	tgBot.Handle("/roast_random", bot.handleRoastUser)
	tgBot.Handle("/reminder_random", bot.handleReminder)
	tgBot.Handle("/top_mat", bot.handleTopMat)
	tgBot.Handle(telebot.OnUserJoined, bot.handleUserJoined)

	tgBot.Handle(telebot.OnText, func(c telebot.Context) error {
		message := c.Message()

		log.Printf("Получено сообщение от %s (ID: %d) в чате %d (%s): %s",
			message.Sender.Username, message.Sender.ID,
			c.Chat().ID, c.Chat().Title, message.Text)

		bot.saveMessage(message)
		go bot.maybeDoRandomAction(c)

		if strings.Contains(message.Text, "@"+config.BotUsername) {
			log.Printf("Обнаружено упоминание бота в сообщении: %s", message.Text)

			if bot.isRoastMessage(message.Text) {
				log.Printf("Это провокация, отвечаем умнику")
				return bot.handleRoast(c)
			}

			log.Printf("Это запрос резюме")
			return bot.handleSummaryRequest(c)
		}

		return nil
	})

	tgBot.Handle("/debug", func(c telebot.Context) error {
		if !bot.isAdmin(c.Sender().ID) {
			return c.Reply("❌ Только для админов")
		}

		var count int64
		today := time.Now().Truncate(24 * time.Hour)
		tomorrow := today.Add(24 * time.Hour)

		bot.db.Model(&Message{}).
			Where("chat_id = ? AND timestamp >= ? AND timestamp < ?",
				c.Chat().ID, today, tomorrow).
			Count(&count)

		return c.Reply(fmt.Sprintf("💾 Сегодня сохранено сообщений: %d\n📋 Chat ID: %d", count, c.Chat().ID))
	})

	go bot.startHealthServer()

	log.Printf("Бот запущен! Username: @%s", config.BotUsername)
	tgBot.Start()
}
