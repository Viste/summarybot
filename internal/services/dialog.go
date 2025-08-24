package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"summarybot/internal/database"
	"summarybot/internal/utils"
	"time"

	"github.com/sashabaranov/go-openai"
	"gorm.io/gorm"
)

type DialogService struct {
	db      *gorm.DB
	ai      *openai.Client
	model   string
	botName string
}

func NewDialogService(db *gorm.DB, ai *openai.Client, model, botName string) *DialogService {
	return &DialogService{
		db:      db,
		ai:      ai,
		model:   model,
		botName: botName,
	}
}

// GetOrCreateDialog получает или создает новый диалог
func (s *DialogService) GetOrCreateDialog(threadID string, chatID, userID int64, firstName string) (*database.DialogContext, bool) {
	var context database.DialogContext
	err := s.db.Where("thread_id = ?", threadID).Order("message_order DESC").First(&context).Error

	if err == nil {
		return &context, false // Существующий диалог
	}

	// Создаем новый диалог
	gender := s.DetermineGender(firstName)
	return &database.DialogContext{
		ThreadID:      threadID,
		ChatID:        chatID,
		UserID:        userID,
		UserFirstName: firstName,
		UserGender:    gender,
		MessageOrder:  0,
	}, true
}

// SaveDialogMessage сохраняет сообщение в диалоге
func (s *DialogService) SaveDialogMessage(ctx *database.DialogContext, userMessage, botResponse string, botMsgID, userMsgID int, isGreeting bool) error {
	ctx.UserMessage = userMessage
	ctx.BotResponse = botResponse
	ctx.BotMessageID = botMsgID
	ctx.UserMessageID = userMsgID
	ctx.MessageOrder++
	ctx.IsGreeting = isGreeting
	ctx.UpdatedAt = time.Now()

	if ctx.ID == 0 {
		ctx.CreatedAt = time.Now()
		return s.db.Create(ctx).Error
	}

	return s.db.Save(ctx).Error
}

// GetDialogHistory получает историю диалога
func (s *DialogService) GetDialogHistory(threadID string, limit int) ([]database.DialogContext, error) {
	var contexts []database.DialogContext
	err := s.db.Where("thread_id = ?", threadID).
		Order("message_order ASC").
		Limit(limit).
		Find(&contexts).Error

	return contexts, err
}

// GenerateResponse генерирует ответ с учетом контекста
func (s *DialogService) GenerateResponse(message, username, gender string, history []database.DialogContext, isProvocation bool) (string, error) {
	// Проверяем, не было ли уже приветствия в этом диалоге
	hasGreeting := false
	for _, h := range history {
		if h.IsGreeting {
			hasGreeting = true
			break
		}
	}

	systemPrompt := s.buildSystemPrompt(username, gender, history, isProvocation, hasGreeting)

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
					Content: fmt.Sprintf("Пользователь %s написал тебе: \"%s\"\n\nОтветь в своем стиле, учитывая контекст диалога.", username, message),
				},
			},
			MaxTokens:   400,
			Temperature: 0.9,
		},
	)

	if err != nil {
		log.Printf("Ошибка OpenAI API: %v", err)
		if isProvocation {
			return utils.GetRandomRoastResponse(), nil
		}
		return utils.GetRandomFriendlyResponse(), nil
	}

	if len(resp.Choices) == 0 {
		if isProvocation {
			return utils.GetRandomRoastResponse(), nil
		}
		return utils.GetRandomFriendlyResponse(), nil
	}

	return resp.Choices[0].Message.Content, nil
}

func (s *DialogService) buildSystemPrompt(username, gender string, history []database.DialogContext, isProvocation, hasGreeting bool) string {
	genderAddress := utils.GetGenderAddress(gender)

	// Собираем контекст диалога
	var contextText string
	if len(history) > 0 {
		contextText = "\n\nИСТОРИЯ ДИАЛОГА:\n"
		for _, ctx := range history {
			contextText += fmt.Sprintf("Пользователь: %s\nТы: %s\n\n", ctx.UserMessage, ctx.BotResponse)
		}
	}

	lorText := `
ВАЖНАЯ ИНФОРМАЦИЯ О ЧАТЕ И ЛЮДЯХ:
- КФД клан - это лютые парни с Питера, гангстеры убийцы (но это все шутки, конечно)
- Артем (@Headhun) - опер, серьезный чувак
- Заги Бок - отец чата, лучший андерграунд репер, мастер подъебов и самый главный гей
- Ольга - святой человек, душа компании

Используй эту информацию естественно в разговоре, если к месту.`

	basePrompt := fmt.Sprintf(`Ты Нигерок с папироской - %s

ИНФОРМАЦИЯ О ПОЛЬЗОВАТЕЛЕ:
- Имя: %s
- Пол: %s
- Обращайся: %s

%s

%s`,
		s.getPersonality(isProvocation),
		username, gender, genderAddress,
		lorText, contextText)

	// Добавляем инструкцию про приветствие
	if hasGreeting {
		basePrompt += "\n\nВАЖНО: Ты УЖЕ ПОЗДОРОВАЛСЯ в начале диалога! НЕ здоровайся снова, продолжай разговор естественно!"
	}

	if isProvocation {
		basePrompt += s.getProvocationInstructions()
	} else {
		basePrompt += s.getFriendlyInstructions()
	}

	return basePrompt
}

func (s *DialogService) getPersonality(isProvocation bool) string {
	if isProvocation {
		return "крутой пацан с района, лучший друг всех в чате, мастер подъебов."
	}
	return "душевный пацан с района, лучший друг всех в чате КФД."
}

func (s *DialogService) getProvocationInstructions() string {
	return `

ТВОЯ ЗАДАЧА - ответить на провокацию МАКСИМАЛЬНО КРЕАТИВНО и ЖЕСТКО, но дружелюбно.

ПРИНЦИПЫ:
- БУДЬ КРЕАТИВНЫМ! Каждый ответ должен быть УНИКАЛЬНЫМ
- Жесткий троллинг, но дружеский
- 2-3 предложения
- Высмеивай примитивность провокации
- НЕ ЗДОРОВАЙСЯ если уже здоровался в диалоге!`
}

func (s *DialogService) getFriendlyInstructions() string {
	return `

ТВОЯ ЛИЧНОСТЬ:
- Живой, интересный собеседник
- Любишь пошутить и поддержать
- Можешь рассказать что-то интересное
- Задаешь встречные вопросы

СТИЛЬ ОБЩЕНИЯ:
- РАЗНООБРАЗНЫЕ ответы! Никаких шаблонов!
- НЕ ЗДОРОВАЙСЯ если уже здоровался в этом диалоге!
- Продолжай разговор естественно
- 2-4 предложения
- Используй разную длину ответов`
}

// DetermineGender определяет пол по имени
func (s *DialogService) DetermineGender(firstName string) string {
	if firstName == "" {
		return "unknown"
	}

	firstName = strings.ToLower(firstName)
	femaleEndings := []string{"а", "я", "ь"}
	for _, ending := range femaleEndings {
		if strings.HasSuffix(firstName, ending) && !strings.HasSuffix(firstName, "ль") {
			return "female"
		}
	}

	return "male"
}
