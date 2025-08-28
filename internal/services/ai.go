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

func (s *AIService) GenerateRapNickname(originalName string) (string, error) {
	systemPrompt := `Ты генератор максимально пост-мета-ироничных рэп никнеймов нового поколения.

Твоя задача - создать АБСУРДНО СМЕШНОЙ рэп-ник, который одновременно:
- Высмеивает все клише рэп-культуры
- Настолько абсурдный, что становится крутым
- Содержит несочетаемые элементы
- Максимально иронично-серьезный

СТИЛЬ НИКНЕЙМОВ:
- Микс из: Lil/Young/Big + абсурдное слово + цифры/эмодзи концепт
- Можно: русские слова латиницей, корявый английский
- Примеры стиля: "Lil Borsch 47", "Young Babushka", "Big Shaverma XXL"
- Используй: бытовые предметы, еду, мемы, офисные термины
- Добавляй: случайные цифры, XXL, 2.0, PRO, feat. себя же

ФОРМУЛА АБСУРДА:
1. Возьми что-то максимально НЕ гангстерское
2. Добавь рэп-префикс (Lil/Young/Big/MC/DJ)
3. Приправь циферками или версией
4. Сделай это настолько нелепым, что станет легендарным

ЗАПРЕЩЕНО:
- Реальные оскорбления
- Настоящие крутые никнеймы
- Логичные сочетания

ВАЖНО: 
- Один ник за раз
- Максимум 3-4 слова
- Чем абсурднее, тем лучше
- Это должно быть смешно до слез`

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
					Content: fmt.Sprintf("Придумай максимально пост-ироничный рэп-никнейм для человека по имени '%s' (можешь использовать имя или полностью игнорировать). Главное - максимальный абсурд и юмор!", originalName),
				},
			},
			MaxTokens:   300,
			Temperature: 0.95,
		},
	)

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "MC Glitch 404", nil
	}

	return resp.Choices[0].Message.Content, nil
}
