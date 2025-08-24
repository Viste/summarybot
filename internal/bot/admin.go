package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"summarybot/internal/database"
	"summarybot/internal/utils"
	"time"

	"gopkg.in/telebot.v3"
)

// HandleApprove обработчик команды /approve
func (b *Bot) HandleApprove(c telebot.Context) error {
	if !b.IsAdmin(c.Sender().ID) {
		return c.Reply("⌛ У вас нет прав администратора.")
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		return c.Reply("📍 Использование: <code>/approve &lt;chat_id&gt;</code>", &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}

	chatID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return c.Reply("⌛ Неверный формат chat_id")
	}

	result := b.db.Model(&database.ChatApprovalRequest{}).
		Where("chat_id = ? AND status = 'pending'", chatID).
		Update("status", "approved")

	if result.RowsAffected == 0 {
		return c.Reply("⌛ Запрос не найден или уже обработан")
	}

	var request database.ChatApprovalRequest
	b.db.Where("chat_id = ?", chatID).First(&request)

	allowedChat := database.AllowedChat{
		ChatID:    chatID,
		ChatTitle: request.ChatTitle,
		AddedBy:   c.Sender().ID,
		CreatedAt: time.Now(),
	}

	b.db.Create(&allowedChat)

	return c.Reply(fmt.Sprintf("✅ Чат %d одобрен и добавлен в разрешенные!", chatID))
}

// HandleReject обработчик команды /reject
func (b *Bot) HandleReject(c telebot.Context) error {
	if !b.IsAdmin(c.Sender().ID) {
		return c.Reply("⌛ У вас нет прав администратора.")
	}

	args := strings.Fields(c.Message().Text)
	if len(args) < 2 {
		return c.Reply("📍 Использование: <code>/reject &lt;chat_id&gt;</code>", &telebot.SendOptions{
			ParseMode: telebot.ModeHTML,
		})
	}

	chatID, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return c.Reply("⌛ Неверный формат chat_id")
	}

	result := b.db.Model(&database.ChatApprovalRequest{}).
		Where("chat_id = ? AND status = 'pending'", chatID).
		Update("status", "rejected")

	if result.RowsAffected == 0 {
		return c.Reply("⌛ Запрос не найден или уже обработан")
	}

	return c.Reply(fmt.Sprintf("🚫 Запрос для чата %d отклонен.", chatID))
}

// HandlePending обработчик команды /pending
func (b *Bot) HandlePending(c telebot.Context) error {
	if !b.IsAdmin(c.Sender().ID) {
		return c.Reply("⌛ У вас нет прав администратора.")
	}

	var requests []database.ChatApprovalRequest
	b.db.Where("status = 'pending'").Order("created_at DESC").Find(&requests)

	if len(requests) == 0 {
		return c.Reply("📭 Нет ожидающих запросов.")
	}

	var response strings.Builder
	response.WriteString("📋 <b>Ожидающие запросы:</b>\n\n")

	for _, req := range requests {
		displayName := req.FirstName
		if displayName == "" {
			displayName = req.Username
		}

		response.WriteString(fmt.Sprintf("📍 <b>%s</b> (%d)\n",
			utils.EscapeHTML(req.ChatTitle), req.ChatID))
		response.WriteString(fmt.Sprintf("   👤 %s (%d)\n",
			utils.EscapeHTML(displayName), req.UserID))
		response.WriteString(fmt.Sprintf("   📅 %s\n",
			req.CreatedAt.Format("02.01.2006 15:04")))
		response.WriteString(fmt.Sprintf("   • <code>/approve %d</code> <code>/reject %d</code>\n\n",
			req.ChatID, req.ChatID))
	}

	return c.Reply(response.String(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

// HandleAllowed обработчик команды /allowed
func (b *Bot) HandleAllowed(c telebot.Context) error {
	if !b.IsAdmin(c.Sender().ID) {
		return c.Reply("⌛ У вас нет прав администратора.")
	}

	var chats []database.AllowedChat
	b.db.Order("created_at DESC").Find(&chats)

	var response strings.Builder
	response.WriteString("📋 <b>Разрешенные чаты:</b>\n\n")

	for _, chatID := range b.config.AllowedChats {
		response.WriteString(fmt.Sprintf("📍 %d <i>(из конфига)</i>\n", chatID))
	}

	for _, chat := range chats {
		response.WriteString(fmt.Sprintf("📍 <b>%s</b> (%d)\n",
			utils.EscapeHTML(chat.ChatTitle), chat.ChatID))
		response.WriteString(fmt.Sprintf("   📅 %s\n\n",
			chat.CreatedAt.Format("02.01.2006 15:04")))
	}

	if len(chats) == 0 && len(b.config.AllowedChats) == 0 {
		response.WriteString("📭 Нет разрешенных чатов.")
	}

	return c.Reply(response.String(), &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}

// HandleSummaryRequest обработчик запроса резюме
func (b *Bot) HandleSummaryRequest(c telebot.Context) error {
	message := c.Message()

	if c.Chat().ID > 0 {
		return c.Reply("⌛ Summary доступен только в групповых чатах, братан! 🤖")
	}

	if !b.IsChatAllowed(c.Chat().ID) {
		return b.handleUnauthorizedChat(c)
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

	summary, err := b.summarySvc.GenerateSummary(c.Chat().ID, days)
	if err != nil {
		c.Bot().Delete(statusMsg)
		return c.Reply("Ошибка при создании резюме 😞")
	}

	c.Bot().Delete(statusMsg)

	var count int64
	startDate := time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)
	endDate := startDate.Add(24 * time.Hour)
	b.db.Model(&database.Message{}).
		Where("chat_id = ? AND timestamp >= ? AND timestamp < ?",
			c.Chat().ID, startDate, endDate).
		Count(&count)

	summaryText := fmt.Sprintf("📋 <b>Резюме за %s</b>\n\n%s\n\n<i>Проанализировано сообщений: %d</i>",
		period, summary, count)

	return c.Reply(summaryText, &telebot.SendOptions{
		ParseMode: telebot.ModeHTML,
	})
}
