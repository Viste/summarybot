package utils

import (
	"fmt"
	"strings"

	"gopkg.in/telebot.v3"
)

// GetUserDisplayName возвращает FirstName если он есть, иначе Username
func GetUserDisplayName(user *telebot.User) string {
	if user.FirstName != "" {
		return user.FirstName
	}
	if user.Username != "" {
		return user.Username
	}
	return "Аноним"
}

// CreateUserMention создает упоминание пользователя с ссылкой
func CreateUserMention(user *telebot.User) string {
	displayName := GetUserDisplayName(user)

	// Если есть username, делаем ссылку
	if user.Username != "" {
		return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, user.ID, EscapeHTML(displayName))
	}

	// Если нет username, используем tg://user ссылку по ID
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, user.ID, EscapeHTML(displayName))
}

// CreateUserMentionPlain создает упоминание для plain текста
func CreateUserMentionPlain(user *telebot.User) string {
	if user.Username != "" {
		return "@" + user.Username
	}
	return GetUserDisplayName(user)
}

// EscapeHTML экранирует HTML символы
func EscapeHTML(text string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(text)
}

// TrimBotUsername удаляет упоминание бота из текста
func TrimBotUsername(text, botUsername string) string {
	cleanText := strings.ReplaceAll(text, "@"+botUsername, "")
	cleanText = strings.ReplaceAll(text, "@"+strings.ToLower(botUsername), "")
	return strings.TrimSpace(cleanText)
}

// GenerateThreadID создает уникальный ID для диалога
func GenerateThreadID(chatID, userID int64, timestamp int64) string {
	return fmt.Sprintf("%d_%d_%d", chatID, userID, timestamp)
}

// GetGenderAddress возвращает обращение в зависимости от пола
func GetGenderAddress(gender string) string {
	switch gender {
	case "male":
		return "братан"
	case "female":
		return "подруга"
	default:
		return "дружище"
	}
}

// IsProvocativeMessage проверяет, является ли сообщение провокацией
func IsProvocativeMessage(text string) bool {
	cleanText := strings.ToLower(text)

	roastTriggers := []string{
		"сосал", "сосешь", "соси", "пидор", "гей", "лох",
		"дурак", "идиот", "тупой", "долбоеб", "мудак", "ебан",
		"дебил", "придурок", "кретин", "козел", "свинья", "урод",
		"падла", "говно", "хуй", "пизда", "ебать", "блять",
		"сука", "шлюха", "обосрался", "обосрался", "ублюдок",
		"даун", "аутист", "чмо", "лошара", "терпила",
	}

	for _, trigger := range roastTriggers {
		if strings.Contains(cleanText, trigger) {
			return true
		}
	}

	// Короткие провокационные сообщения с вопросами
	if len(cleanText) <= 15 && strings.Contains(cleanText, "?") {
		// Исключаем нормальные приветствия
		greetings := []string{"как дела", "че как", "живой", "работаешь", "спишь", "ку", "привет"}
		for _, greeting := range greetings {
			if strings.Contains(cleanText, greeting) {
				return false
			}
		}

		// Если много вопросов и мало текста - вероятно провокация
		if strings.Count(cleanText, "?") >= 2 ||
			(strings.Count(cleanText, "?") >= 1 && len(strings.TrimSpace(strings.ReplaceAll(cleanText, "?", ""))) <= 5) {
			return true
		}
	}

	return false
}

// IsSummaryRequest проверяет, является ли сообщение запросом резюме
func IsSummaryRequest(text string) bool {
	cleanText := strings.ToLower(text)

	summaryTriggers := []string{
		"что было", "что происходило", "резюме", "саммари", "summary",
		"сегодня", "вчера", "позавчера",
		"дн", "день", "дня", "дней",
	}

	for _, trigger := range summaryTriggers {
		if strings.Contains(cleanText, trigger) {
			return true
		}
	}

	return false
}
