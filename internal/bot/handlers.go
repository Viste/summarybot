package bot

import (
	"fmt"
	"log"
	"strings"
	"summarybot/internal/database"
	"summarybot/internal/utils"
	"time"

	"gopkg.in/telebot.v3"
)

// HandleStart обработчик команды /start
func (b *Bot) HandleStart(c telebot.Context) error {
	if c.Chat().ID > 0 {
		if b.IsAdmin(c.Sender().ID) {
			return c.Reply(getAdminWelcomeText(), &telebot.SendOptions{
				ParseMode: telebot.ModeHTML,
			})
		}
		return c.Reply(getPrivateWelcomeText(), &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}

	if !b.IsChatAllowed(c.Chat().ID) {
		return b.handleUnauthorizedChat(c)
	}

	return c.Reply(getGroupWelcomeText(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

// HandleUserJoined обработчик входа новых пользователей
func (b *Bot) HandleUserJoined(c telebot.Context) error {
	if c.Chat().ID > 0 || !b.IsChatAllowed(c.Chat().ID) {
		return nil
	}

	for _, user := range c.Message().UsersJoined {
		if user.IsBot {
			continue
		}

		// Используем FirstName для приветствия
		displayName := utils.GetUserDisplayName(&user)
		mention := utils.CreateUserMention(&user)

		// Получаем уникальное приветствие
		greeting := b.greetingGen.GetUniqueGreeting(utils.EscapeHTML(displayName))

		// Заменяем имя на mention со ссылкой
		greeting = strings.Replace(greeting, utils.EscapeHTML(displayName), mention, 1)

		c.Reply(greeting, &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})

		log.Printf("Приветствие для нового пользователя: %s (ID: %d)", displayName, user.ID)
	}

	return nil
}

// HandleMentions обработчик упоминаний бота
func (b *Bot) HandleMentions(c telebot.Context) error {
	message := c.Message()

	log.Printf("Обнаружено упоминание бота от %s: %s",
		utils.GetUserDisplayName(message.Sender), message.Text)

	// Проверяем, это запрос резюме?
	if utils.IsSummaryRequest(message.Text) {
		return b.HandleSummaryRequest(c)
	}

	// Создаем новый диалог
	threadID := utils.GenerateThreadID(c.Chat().ID, message.Sender.ID, time.Now().Unix())

	isProvocation := utils.IsProvocativeMessage(message.Text)

	// Получаем правильное имя пользователя
	displayName := utils.GetUserDisplayName(message.Sender)

	// Генерируем ответ
	response, err := b.dialogSvc.GenerateResponse(
		message.Text,
		displayName,
		b.dialogSvc.DetermineGender(message.Sender.FirstName),
		[]database.DialogContext{},
		isProvocation,
	)

	if err != nil {
		log.Printf("Ошибка генерации ответа: %v", err)
		response = "Братан, не расслышал! Повтори еще раз 👂"
	}

	sentMessage, err := c.Bot().Send(c.Chat(), response, &telebot.SendOptions{
		ReplyTo: message,
	})

	if err != nil {
		return err
	}

	// Сохраняем начало диалога
	ctx := &database.DialogContext{
		ThreadID:      threadID,
		ChatID:        c.Chat().ID,
		UserID:        message.Sender.ID,
		UserFirstName: message.Sender.FirstName,
		UserGender:    b.dialogSvc.DetermineGender(message.Sender.FirstName),
	}

	b.dialogSvc.SaveDialogMessage(
		ctx,
		message.Text,
		response,
		sentMessage.ID,
		message.ID,
		true, // Это первое сообщение, может содержать приветствие
	)

	return nil
}

// HandleBotReply обработчик ответов на сообщения бота
func (b *Bot) HandleBotReply(c telebot.Context) error {
	message := c.Message()

	// Проверяем, действительно ли это ответ на наше сообщение
	if message.ReplyTo == nil || message.ReplyTo.Sender.Username != b.config.BotUsername {
		return nil
	}

	// Ищем контекст диалога
	var dialogCtx database.DialogContext
	err := b.db.Where("chat_id = ? AND bot_message_id = ?",
		c.Chat().ID, message.ReplyTo.ID).
		Order("created_at DESC").
		First(&dialogCtx).Error

	if err != nil {
		// Создаем новый диалог если не нашли
		threadID := utils.GenerateThreadID(c.Chat().ID, message.Sender.ID, time.Now().Unix())
		dialogCtx = database.DialogContext{
			ThreadID:      threadID,
			ChatID:        c.Chat().ID,
			UserID:        message.Sender.ID,
			UserFirstName: message.Sender.FirstName,
			UserGender:    b.dialogSvc.DetermineGender(message.Sender.FirstName),
		}
	}

	// Получаем историю диалога
	history, _ := b.dialogSvc.GetDialogHistory(dialogCtx.ThreadID, 10)

	displayName := utils.GetUserDisplayName(message.Sender)
	isProvocation := utils.IsProvocativeMessage(message.Text)

	// Генерируем ответ с учетом контекста
	response, err := b.dialogSvc.GenerateResponse(
		message.Text,
		displayName,
		dialogCtx.UserGender,
		history,
		isProvocation,
	)

	if err != nil {
		log.Printf("Ошибка генерации ответа: %v", err)
		response = "Секунду, обрабатываю... 🤔"
	}

	sentMessage, err := c.Bot().Send(c.Chat(), response, &telebot.SendOptions{
		ReplyTo: message,
	})

	if err != nil {
		return err
	}

	// Сохраняем продолжение диалога
	b.dialogSvc.SaveDialogMessage(
		&dialogCtx,
		message.Text,
		response,
		sentMessage.ID,
		message.ID,
		false, // Это не первое сообщение
	)

	log.Printf("Ответ в диалоге thread %s сохранен", dialogCtx.ThreadID)

	return nil
}

// HandleRoastRandom обработчик команды /roast_random
func (b *Bot) HandleRoastRandom(c telebot.Context) error {
	if c.Chat().ID > 0 || !b.IsChatAllowed(c.Chat().ID) {
		return c.Reply("⌛ Подколы только в групповых чатах!")
	}

	user, err := b.statsSvc.GetRandomActiveUser(c.Chat().ID)
	if err != nil {
		return c.Reply("😔 Некого подколоть - в чате тишина!")
	}

	// Создаем правильное упоминание
	mention := utils.CreateUserMention(user)

	roast, err := b.aiSvc.GenerateRoast(utils.GetUserDisplayName(user))
	if err != nil {
		roast = "Даже я не знаю как тебя подколоть, братан 😂"
	}

	message := fmt.Sprintf("%s %s", mention, roast)

	return c.Reply(message, &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

// HandleTopMat обработчик команды /top_mat
func (b *Bot) HandleTopMat(c telebot.Context) error {
	if c.Chat().ID > 0 || !b.IsChatAllowed(c.Chat().ID) {
		return c.Reply("⌛ Статистика мата только в групповых чатах!")
	}

	stats := b.statsSvc.GetTopSwearers(c.Chat().ID, 10)

	if len(stats) == 0 {
		return c.Reply("🤯 Невероятно! В этом чате еще никто не матерился! 😇")
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

		displayName := stat.FirstName
		if displayName == "" {
			displayName = stat.Username
		}

		response.WriteString(fmt.Sprintf("%s <b>%s</b> - %d раз\n",
			medal, utils.EscapeHTML(displayName), stat.Total))
	}

	response.WriteString("\n<i>Статистика ведется с момента последнего обновления бота 📊</i>")

	return c.Reply(response.String(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

// Вспомогательные функции для текстов
func getAdminWelcomeText() string {
	return `Привет, админ! 👑

<b>Доступные команды:</b>
• /approve &lt;chat_id&gt; - одобрить чат
• /reject &lt;chat_id&gt; - отклонить запрос
• /pending - показать ожидающие запросы
• /allowed - список разрешенных чатов
• /help - подробная помощь

<b>В групповых чатах также доступны:</b>
• /roast_random - жесткий подкол случайному корешу 🔥
• /reminder_random - "важное" напоминание кому-то 😁
• /top_mat - топ матершинников 🤬

Summary доступен только в групповых чатах! 🤖`
}

func getPrivateWelcomeText() string {
	return `👋 <b>Привет!</b>

Этот бот работает только в групповых чатах.
Добавь меня в группу и попроси резюме!

Используй /help для подробной информации 📖`
}

func getGroupWelcomeText() string {
	return `Привет! 👋 

Я бот для создания резюме чата и общения! 

<b>Основные команды:</b>
• @zagichak_bot что было за сегодня - резюме
• @zagichak_bot привет - просто поболтать
• Отвечай на мои сообщения - будем диалог вести! 💬
• /roast_random - подкол случайному корешу 🔥
• /reminder_random - напоминание кому-то 😁
• /top_mat - топ матершинников 🤬

Я теперь помню контекст диалогов и знаю кто есть кто в чате! 
Используй /help для подробной помощи! 🤖✨`
}

func (b *Bot) handleUnauthorizedChat(c telebot.Context) error {
	if b.config.RequireApproval && c.Chat().ID < 0 {
		chatTitle := c.Chat().Title
		if chatTitle == "" {
			chatTitle = "Неизвестный чат"
		}

		b.RequestChatApproval(c.Chat().ID, chatTitle, c.Sender().ID,
			c.Sender().Username, c.Sender().FirstName)

		return c.Reply("⌛ Доступ к этому чату не разрешен.\n\n" +
			"📍 Запрос на одобрение отправлен администраторам.\n" +
			"⏳ Ожидайте подтверждения доступа.")
	}

	return c.Reply("⌛ У меня нет доступа к этому чату.")
}
