package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"summarybot/internal/bot"
	"summarybot/internal/config"
	"summarybot/internal/database"
	"summarybot/internal/services"
	"summarybot/internal/utils"
	"time"

	"github.com/sashabaranov/go-openai"
	"gopkg.in/telebot.v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	// конфигурация
	cfg := config.Load()

	// бд
	db, err := initDatabase(cfg.DatabasePath)
	if err != nil {
		log.Fatalf("Ошибка инициализации БД: %v", err)
	}

	// OpenAI клиент
	openaiConfig := openai.DefaultConfig(cfg.OpenAIAPIKey)
	if cfg.OpenAIBaseURL != "" {
		openaiConfig.BaseURL = cfg.OpenAIBaseURL
	}
	openaiClient := openai.NewClientWithConfig(openaiConfig)

	// сервисы
	dialogSvc := services.NewDialogService(db, openaiClient, cfg.OpenAIModel, cfg.BotUsername)
	summarySvc := services.NewSummaryService(db, openaiClient, cfg.OpenAIModel, cfg.MinMessagesForAI)
	statsSvc := services.NewStatsService(db)
	aiSvc := services.NewAIService(openaiClient, cfg.OpenAIModel)

	// бот
	pref := telebot.Settings{
		Token:  cfg.TelegramToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	tgBot, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatalf("Ошибка создания Telegram бота: %v", err)
	}

	botApp := bot.New(cfg, db, tgBot, dialogSvc, summarySvc, statsSvc, aiSvc)

	// обработчики
	registerHandlers(tgBot, botApp, cfg)

	// health сервер
	go startHealthServer(cfg.Port)

	log.Printf("Бот запущен! Username: @%s", cfg.BotUsername)
	tgBot.Start()
}

func initDatabase(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	// мигрируеммодели
	err = db.AutoMigrate(
		&database.Message{},
		&database.ChatSummary{},
		&database.AllowedChat{},
		&database.ChatApprovalRequest{},
		&database.SwearStats{},
		&database.DialogContext{},
		&database.UsedGreeting{},
	)

	return db, err
}

func registerHandlers(tgBot *telebot.Bot, botApp *bot.Bot, cfg *config.Config) {
	// команды
	tgBot.Handle("/start", botApp.HandleStart)
	tgBot.Handle("/help", botApp.HandleHelp)
	tgBot.Handle("/roast_random", botApp.HandleRoastRandom)
	tgBot.Handle("/reminder_random", botApp.HandleReminderRandom)
	tgBot.Handle("/top_mat", botApp.HandleTopMat)
	tgBot.Handle("/rap_nik", botApp.HandleRapNik)
	// админские
	tgBot.Handle("/approve", botApp.HandleApprove)
	tgBot.Handle("/reject", botApp.HandleReject)
	tgBot.Handle("/pending", botApp.HandlePending)
	tgBot.Handle("/allowed", botApp.HandleAllowed)
	tgBot.Handle(telebot.OnUserJoined, botApp.HandleUserJoined)
	tgBot.Handle(telebot.OnText, func(c telebot.Context) error {
		message := c.Message()
		botApp.SaveMessage(message)
		go botApp.MaybeDoRandomAction(c)
		if message.ReplyTo != nil && message.ReplyTo.Sender.Username == cfg.BotUsername {
			log.Printf("Обнаружен reply на сообщение бота от %s",
				utils.GetUserDisplayName(message.Sender))
			return botApp.HandleBotReply(c)
		}
		if strings.Contains(message.Text, "@"+cfg.BotUsername) {
			log.Printf("Обнаружено упоминание бота в сообщении от %s",
				utils.GetUserDisplayName(message.Sender))
			return botApp.HandleMentions(c)
		}

		return nil
	})
}

func startHealthServer(port string) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	log.Printf("Health server запущен на порту %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Printf("Ошибка health сервера: %v", err)
	}
}
