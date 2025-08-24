package services

import (
	"context"
	"fmt"
	"strings"
	"summarybot/internal/database"
	"time"

	"github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

type SummaryService struct {
	db               *gorm.DB
	ai               *openai.Client
	model            string
	minMessagesForAI int
}

func NewSummaryService(db *gorm.DB, ai *openai.Client, model string, minMessages int) *SummaryService {
	return &SummaryService{
		db:               db,
		ai:               ai,
		model:            model,
		minMessagesForAI: minMessages,
	}
}

func (s *SummaryService) GenerateSummary(chatID int64, days int) (string, error) {
	messages, err := s.getMessagesForPeriod(chatID, days)
	if err != nil {
		return "", err
	}

	period := s.getPeriodName(days)

	if len(messages) == 0 {
		return fmt.Sprintf("За %s никто ничего не писал, братан 🤷‍♂️", period), nil
	}

	if len(messages) < s.minMessagesForAI {
		return fmt.Sprintf("За %s было всего %d сообщений - слишком мало для нормального резюме, братан 📱\n\n"+
			"Попробуй запросить резюме когда народ побольше пообщается! (нужно минимум %d сообщений)",
			period, len(messages), s.minMessagesForAI), nil
	}

	var textBuilder strings.Builder
	for _, msg := range messages {
		displayName := msg.FirstName
		if displayName == "" {
			displayName = msg.Username
		}
		textBuilder.WriteString(fmt.Sprintf("[%s] %s: %s\n",
			msg.Timestamp.Format("15:04"), displayName, msg.Text))
	}

	summary, err := s.generateAISummary(textBuilder.String(), period, len(messages))
	if err != nil {
		return "Не смог замутить резюме, братан 😞", err
	}

	s.saveSummary(chatID, days, summary)

	return summary, nil
}

func (s *SummaryService) getMessagesForPeriod(chatID int64, days int) ([]database.Message, error) {
	var messages []database.Message
	startDate := time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour)
	endDate := startDate.Add(24 * time.Hour)

	err := s.db.Where("chat_id = ? AND timestamp >= ? AND timestamp < ?",
		chatID, startDate, endDate).
		Order("timestamp ASC").
		Find(&messages).Error

	return messages, err
}

func (s *SummaryService) getPeriodName(days int) string {
	switch days {
	case 0:
		return "сегодня"
	case 1:
		return "вчера"
	case 2:
		return "позавчера"
	default:
		return fmt.Sprintf("%d дней назад", days)
	}
}

func (s *SummaryService) generateAISummary(messages, period string, count int) (string, error) {
	systemPrompt := `Ты крутой пацан с района, который умеет анализировать чатики и делать огненные резюме для корешей.

ВАЖНО - АНАЛИЗИРУЙ ТОЛЬКО РЕАЛЬНЫЕ СООБЩЕНИЯ:
- Пересказывай ТОЛЬКО то, что реально было написано в чате
- НЕ выдумывай события, имена, темы которых не было
- Если сообщений мало или они скучные - честно говори об этом
- Точно передавай факты, но своими словами в классном стиле
- НИКОГДА НЕ ПОВТОРЯЙ одну и ту же информацию в разных секциях!

Твой стиль:
- Говоришь как настоящий братан - простым языком, с прикольными фразочками
- Используешь сленг: "братан", "чел", "тема", "движ", "кайф", "жесть" и т.д.
- Эмодзи ставишь к месту, но не переборщиваешь
- Пишешь живо и интересно, как будто рассказываешь корешу что было
- Если что-то скучное - честно говоришь об этом

Что ты делаешь:
- Выделяешь 4-8 РАЗНЫХ тем/событий ИЗ РЕАЛЬНЫХ СООБЩЕНИЙ
- Каждая тема должна быть УНИКАЛЬНОЙ - не повторяй информацию!
- Группируешь связанные сообщения, но не дублируй их в разных секциях
- Используешь HTML теги: <b>жирный</b>, <i>курсив</i>
- Пишешь 1-2 предложения на тему, коротко и по делу

НОВЫЙ упрощенный формат (БЕЗ ПОВТОРОВ!):

🔥 <b>Главные темы дня:</b>
• [тема 1 с эмодзи] - описание
• [тема 2 с эмодзи] - описание  
• [тема 3 с эмодзи] - описание
• [тема 4 с эмодзи] - описание (если есть)

📍 <b>Полезняк:</b> (только если реально есть ссылки/важная инфа)
• [ссылка или важное решение]

Главное - каждая тема должна быть РАЗНОЙ! Не повторяй одно и то же!`

	userPrompt := fmt.Sprintf(`Проанализируй ВСЕ сообщения ниже и сделай резюме за %s. 

ВАЖНО: Анализируй ТОЛЬКО эти сообщения, не выдумывай ничего лишнего!

Всего сообщений для анализа: %d

Сообщения:
%s`, period, count, messages)

	resp, err := s.ai.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model: s.model,
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
			MaxTokens:   1200,
			Temperature: 0.3,
		},
	)

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("пустой ответ от AI")
	}

	return resp.Choices[0].Message.Content, nil
}

func (s *SummaryService) saveSummary(chatID int64, days int, summary string) {
	chatSummary := database.ChatSummary{
		ChatID:    chatID,
		Date:      time.Now().AddDate(0, 0, -days).Truncate(24 * time.Hour),
		Summary:   summary,
		CreatedAt: time.Now(),
	}
	s.db.Create(&chatSummary)
}
