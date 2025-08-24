package database

import (
	"time"
)

type Message struct {
	ID        uint  `gorm:"primaryKey"`
	ChatID    int64 `gorm:"index"`
	UserID    int64 `gorm:"index"`
	Username  string
	FirstName string
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
	FirstName string
	Status    string `gorm:"default:'pending'"`
	CreatedAt time.Time
}

type SwearStats struct {
	ID        uint  `gorm:"primaryKey"`
	ChatID    int64 `gorm:"index"`
	UserID    int64 `gorm:"index"`
	Username  string
	FirstName string
	SwearWord string
	Count     int `gorm:"default:1"`
	UpdatedAt time.Time
}

type DialogContext struct {
	ID            uint   `gorm:"primaryKey"`
	ChatID        int64  `gorm:"index"`
	UserID        int64  `gorm:"index"`
	ThreadID      string `gorm:"index"`
	BotMessageID  int
	UserMessageID int
	UserMessage   string `gorm:"type:text"`
	BotResponse   string `gorm:"type:text"`
	UserGender    string
	UserFirstName string
	MessageOrder  int
	IsGreeting    bool `gorm:"default:false"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Новая модель для отслеживания использованных приветствий
type UsedGreeting struct {
	ID        uint      `gorm:"primaryKey"`
	ChatID    int64     `gorm:"index"`
	UserID    int64     `gorm:"index"`
	Greeting  string    `gorm:"type:text"`
	UsedAt    time.Time `gorm:"index"`
	CreatedAt time.Time
}
