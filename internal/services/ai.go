package services

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

type AIService struct {
	client *openai.Client
	model  string
}

func NewAIService(client *openai.Client, model string) *AIService {
	return &AIService{
		client: client,
		model:  model,
	}
}

func (s *AIService) GenerateRoast(username string) (string, error) {
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

	resp, err := s.client.CreateChatCompletion(
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
					Content: fmt.Sprintf("Сделай максимально жесткий, но дружеский подкол пользователю с именем \"%s\". Это дружеский чат, все кореши, можно жестко тролить!", username),
				},
			},
			MaxTokens:   200,
			Temperature: 0.8,
		},
	)

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "Даже я не знаю как тебя подколоть, братан 😂", nil
	}

	return resp.Choices[0].Message.Content, nil
}

func (s *AIService) GenerateReminder(username string) (string, error) {
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

	resp, err := s.client.CreateChatCompletion(
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
					Content: fmt.Sprintf("Придумай жесткое \"напоминание\"-прикол для пользователя \"%s\". Это должно быть смешно и абсурдно!", username),
				},
			},
			MaxTokens:   150,
			Temperature: 0.9,
		},
	)

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return fmt.Sprintf("Эй %s, ты забыл покормить свою лень! 😴", username), nil
	}

	return resp.Choices[0].Message.Content, nil
}
