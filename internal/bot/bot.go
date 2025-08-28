package bot

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"summarybot/internal/config"
	"summarybot/internal/database"
	"summarybot/internal/services"
	"summarybot/internal/utils"
	"time"

	"gopkg.in/telebot.v3"
	"gorm.io/gorm"
)

type Bot struct {
	config      *config.Config
	db          *gorm.DB
	telebot     *telebot.Bot
	dialogSvc   *services.DialogService
	summarySvc  *services.SummaryService
	statsSvc    *services.StatsService
	aiSvc       *services.AIService
	greetingGen *utils.GreetingGenerator
}

// New создает новый экземпляр бота
func New(
	cfg *config.Config,
	db *gorm.DB,
	tgBot *telebot.Bot,
	dialogSvc *services.DialogService,
	summarySvc *services.SummaryService,
	statsSvc *services.StatsService,
	aiSvc *services.AIService,
) *Bot {
	return &Bot{
		config:      cfg,
		db:          db,
		telebot:     tgBot,
		dialogSvc:   dialogSvc,
		summarySvc:  summarySvc,
		statsSvc:    statsSvc,
		aiSvc:       aiSvc,
		greetingGen: utils.NewGreetingGenerator(),
	}
}

// SaveMessage сохраняет сообщение в БД
func (b *Bot) SaveMessage(m *telebot.Message) {
	if m.Text == "" {
		return
	}

	if !b.IsChatAllowed(m.Chat.ID) {
		return
	}

	message := database.Message{
		ChatID:    m.Chat.ID,
		UserID:    m.Sender.ID,
		Username:  m.Sender.Username,
		FirstName: m.Sender.FirstName,
		Text:      m.Text,
		Timestamp: time.Unix(m.Unixtime, 0),
		CreatedAt: time.Now(),
	}

	if err := b.db.Create(&message).Error; err != nil {
		log.Printf("Ошибка сохранения сообщения: %v", err)
	} else {
		log.Printf("Сообщение сохранено: чат %d, пользователь %s (ID: %d)",
			m.Chat.ID, utils.GetUserDisplayName(m.Sender), m.Sender.ID)
	}

	b.checkAndSaveSwearStats(m)
}

// checkAndSaveSwearStats проверяет сообщение на мат и сохраняет статистику
func (b *Bot) checkAndSaveSwearStats(m *telebot.Message) {
	if m.Chat.ID > 0 {
		return
	}

	swearWords := []string{
		"блять", "хуй", "пизда", "ебать", "сука", "говно", "дерьмо",
		"мудак", "долбоеб", "ублюдок", "сволочь", "падла", "гавно",
		"хрен", "херня", "охуеть", "заебать", "проебать", "наебать",
		"пиздец", "ебаный", "хуевый", "пиздатый", "ебучий", "сраный",
		"бля", "ебло", "хуило", "пидор", "пидарас", "гандон",
	}

	text := strings.ToLower(m.Text)
	for _, swear := range swearWords {
		if strings.Contains(text, swear) {
			var stat database.SwearStats
			result := b.db.Where("chat_id = ? AND user_id = ? AND swear_word = ?",
				m.Chat.ID, m.Sender.ID, swear).First(&stat)

			if result.Error == nil {
				b.db.Model(&stat).Updates(database.SwearStats{
					Count:     stat.Count + 1,
					FirstName: m.Sender.FirstName,
					UpdatedAt: time.Now(),
				})
			} else {
				newStat := database.SwearStats{
					ChatID:    m.Chat.ID,
					UserID:    m.Sender.ID,
					Username:  m.Sender.Username,
					FirstName: m.Sender.FirstName,
					SwearWord: swear,
					Count:     1,
					UpdatedAt: time.Now(),
				}
				b.db.Create(&newStat)
			}
		}
	}
}

// IsChatAllowed проверяет, разрешен ли чат
func (b *Bot) IsChatAllowed(chatID int64) bool {
	// Проверяем в конфиге
	for _, allowedID := range b.config.AllowedChats {
		if allowedID == chatID {
			return true
		}
	}

	var count int64
	b.db.Model(&database.AllowedChat{}).Where("chat_id = ?", chatID).Count(&count)
	return count > 0
}

// IsAdmin проверяет, является ли пользователь админом
func (b *Bot) IsAdmin(userID int64) bool {
	for _, adminID := range b.config.AdminUserIDs {
		if adminID == userID {
			return true
		}
	}
	return false
}

// RequestChatApproval создает запрос на одобрение чата
func (b *Bot) RequestChatApproval(chatID int64, chatTitle string, userID int64, username, firstName string) {
	// Проверяем, нет ли уже запроса
	var existingRequest database.ChatApprovalRequest
	if b.db.Where("chat_id = ? AND status = 'pending'", chatID).First(&existingRequest).Error == nil {
		return
	}

	request := database.ChatApprovalRequest{
		ChatID:    chatID,
		ChatTitle: chatTitle,
		UserID:    userID,
		Username:  username,
		FirstName: firstName,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	b.db.Create(&request)
	b.notifyAdminsAboutNewRequest(request)
}

// notifyAdminsAboutNewRequest уведомляет админов о новом запросе
func (b *Bot) notifyAdminsAboutNewRequest(request database.ChatApprovalRequest) {
	if len(b.config.AdminUserIDs) == 0 {
		return
	}

	displayName := request.FirstName
	if displayName == "" {
		displayName = request.Username
	}

	message := fmt.Sprintf("🔒 <b>Новый запрос доступа</b>\n\n"+
		"<b>Чат:</b> %s (%d)\n"+
		"<b>Пользователь:</b> %s (%d)\n\n"+
		"Используйте команды:\n"+
		"• <code>/approve %d</code> - разрешить\n"+
		"• <code>/reject %d</code> - отклонить\n"+
		"• <code>/pending</code> - показать все запросы",
		utils.EscapeHTML(request.ChatTitle), request.ChatID,
		utils.EscapeHTML(displayName), request.UserID,
		request.ChatID, request.ChatID)

	for _, adminID := range b.config.AdminUserIDs {
		chat := &telebot.Chat{ID: adminID}
		b.telebot.Send(chat, message, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}
}

// MaybeDoRandomAction случайно выполняет какое-то действие
func (b *Bot) MaybeDoRandomAction(c telebot.Context) {
	if c.Chat().ID > 0 || !b.IsChatAllowed(c.Chat().ID) {
		return
	}

	// 1% шанс
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

	user, err := b.statsSvc.GetRandomActiveUser(c.Chat().ID)
	if err != nil {
		return
	}

	mention := utils.CreateUserMention(user)

	if actionType == 0 {
		roast, err := b.aiSvc.GenerateRoast(utils.GetUserDisplayName(user))
		if err != nil {
			return
		}

		message := fmt.Sprintf("%s %s", mention, roast)
		c.Bot().Send(c.Chat(), message, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})

		log.Printf("Автоматический подкол для %s в чате %d",
			utils.GetUserDisplayName(user), c.Chat().ID)
	} else {
		reminder, err := b.aiSvc.GenerateReminder(utils.GetUserDisplayName(user))
		if err != nil {
			return
		}

		message := fmt.Sprintf("🔔 <b>Срочное напоминание:</b>\n\n%s %s",
			mention, reminder)
		c.Bot().Send(c.Chat(), message, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})

		log.Printf("Автоматическое напоминание для %s в чате %d",
			utils.GetUserDisplayName(user), c.Chat().ID)
	}
}

// HandleHelp обработчик команды /help
func (b *Bot) HandleHelp(c telebot.Context) error {
	// Приватный чат
	if c.Chat().ID > 0 {
		if b.IsAdmin(c.Sender().ID) {
			return c.Reply(getAdminHelpText(), &telebot.SendOptions{
				ParseMode: telebot.ModeHTML,
			})
		}
		return c.Reply(getPrivateHelpText(), &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}

	// Групповой чат
	if !b.IsChatAllowed(c.Chat().ID) {
		return c.Reply("⌛ У меня нет доступа к этому чату.\n\n" +
			"Обратитесь к администратору для получения доступа.")
	}

	return c.Reply(getGroupHelpText(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

// HandleReminderRandom обработчик команды /reminder_random
func (b *Bot) HandleReminderRandom(c telebot.Context) error {
	if c.Chat().ID > 0 || !b.IsChatAllowed(c.Chat().ID) {
		return c.Reply("⌛ Напоминания только в групповых чатах!")
	}

	user, err := b.statsSvc.GetRandomActiveUser(c.Chat().ID)
	if err != nil {
		return c.Reply("😔 Некому напоминать - в чате тишина!")
	}

	mention := utils.CreateUserMention(user)

	reminder, err := b.aiSvc.GenerateReminder(utils.GetUserDisplayName(user))
	if err != nil {
		reminder = "Забыл что хотел напомнить 🤪"
	}

	message := fmt.Sprintf("⏰ <b>Важное напоминание:</b>\n\n%s %s", mention, reminder)

	return c.Reply(message, &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

func (b *Bot) HandleRapNik(c telebot.Context) error {
	user := c.Sender()
	displayName := utils.GetUserDisplayName(user)
	mention := utils.CreateUserMention(user)

	if c.Chat().ID < 0 && !b.IsChatAllowed(c.Chat().ID) {
		return c.Reply("⌛ У меня нет доступа к этому чату.")
	}

	nickname, err := b.aiSvc.GenerateRapNickname(displayName)
	if err != nil {
		nicknames := []string{
			"MC Error 500 feat. Глюк",
			"Young 404 Not Found",
			"Defitsit 1991",
			"Excel Killer XXL",
			"Borsch Gang 47",
		}
		nickname = nicknames[rand.Intn(len(nicknames))]
	}

	var message string
	if c.Chat().ID < 0 {
		message = fmt.Sprintf("🎤 <b>Внимание! Рэп-крещение!</b>\n\n"+
			"%s отныне в хип-хоп игре известен как:\n\n"+
			"🔥 <b>%s</b> 🔥\n\n"+
			"<i>Респект новой легенде андерграунда!</i> 💿",
			mention, nickname)
	} else {
		message = fmt.Sprintf("🎤 <b>Твой новый рэп-псевдоним:</b>\n\n"+
			"🔥 <b>%s</b> 🔥\n\n"+
			"<i>Теперь ты готов покорять чарты!</i> 💿", nickname)
	}

	return c.Reply(message, &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

// Вспомогательные тексты
func getAdminHelpText() string {
	return `🤖 <b>Помощь по боту (Админ)</b>

<b>Админские команды:</b>
• /approve &lt;chat_id&gt; - одобрить чат
• /reject &lt;chat_id&gt; - отклонить запрос  
• /pending - показать ожидающие запросы
• /allowed - список разрешенных чатов

<b>В групповых чатах:</b>
• @zagichak_bot что было за сегодня/вчера - резюме чата
• @zagichak_bot [любое сообщение] - общение с ботом
• Отвечай на сообщения бота - веди диалог! 💬
• /roast_random - подкол случайному пользователю 🔥
• /reminder_random - напоминание кому-то 😁  
• /top_mat - топ матершинников чата 🤬
• /rap_nik - генератор рэп-псевдонимов 🎤


Бот работает только в разрешенных групповых чатах! 🤖`
}

func getPrivateHelpText() string {
	return `🤖 <b>Помощь по боту</b>

👋 Этот бот работает только в групповых чатах!

Добавь меня в группу и попробуй:
• @zagichak_bot что было за сегодня - резюме чата
• @zagichak_bot привет - просто поболтать
• Отвечай на мои сообщения - ведем диалог! 💬
• /roast_random - подколоть кого-то 🔥

Я анализирую сообщения и выдам самое интересное! ✨`
}

func getGroupHelpText() string {
	return `🤖 <b>Помощь по боту</b>

<b>Резюме чата:</b>
• @zagichak_bot что было за сегодня
• @zagichak_bot что было за вчера  
• @zagichak_bot что было за позавчера
• @zagichak_bot что было за 3 дня (макс 7)

<b>Общение:</b>
• @zagichak_bot [любое сообщение] - поболтать с ботом
• Отвечай на мои сообщения - ведем диалог! 💬
• Я помню контекст разговора и знаю всех в чате! 🧠

<b>Развлечения:</b>
• /roast_random - жесткий подкол случайному корешу 🔥
• /reminder_random - "важное" напоминание кому-то 😁
• /top_mat - топ матершинников чата 🤬
• /rap_nik - генератор рэп-псевдонимов 🎤

Я анализирую сообщения, делаю крутые резюме и веду живые диалоги! 🤖✨`
}
