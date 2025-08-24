package services

import (
	"fmt"
	"math/rand"
	"time"

	"gopkg.in/telebot.v3"
	"gorm.io/gorm"
)

type StatsService struct {
	db *gorm.DB
}

func NewStatsService(db *gorm.DB) *StatsService {
	return &StatsService{db: db}
}

type UserInfo struct {
	ID        int64
	Username  string
	FirstName string
}

func (s *StatsService) GetRandomActiveUser(chatID int64) (*telebot.User, error) {
	var users []struct {
		UserID    int64
		Username  string
		FirstName string
		Count     int64
	}

	fourteenDaysAgo := time.Now().AddDate(0, 0, -14)

	query := `
		SELECT user_id, username, first_name, COUNT(*) as count 
		FROM messages 
		WHERE chat_id = ? AND timestamp >= ? 
			AND (username != '' OR first_name != '')
		GROUP BY user_id, username, first_name
		HAVING count >= 2
		ORDER BY count DESC 
		LIMIT 30
	`

	err := s.db.Raw(query, chatID, fourteenDaysAgo).Scan(&users).Error
	if err != nil {
		return nil, err
	}

	if len(users) == 0 {
		// Fallback на 30 дней
		thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
		err = s.db.Raw(query, chatID, thirtyDaysAgo).Scan(&users).Error
		if err != nil || len(users) == 0 {
			return nil, fmt.Errorf("нет активных пользователей")
		}
	}

	randomIndex := rand.Intn(len(users))
	selected := users[randomIndex]

	return &telebot.User{
		ID:        selected.UserID,
		Username:  selected.Username,
		FirstName: selected.FirstName,
	}, nil
}

type SwearStat struct {
	Username  string
	FirstName string
	Total     int
}

func (s *StatsService) GetTopSwearers(chatID int64, limit int) []SwearStat {
	var stats []SwearStat

	s.db.Raw(`
		SELECT username, first_name, SUM(count) as total 
		FROM swear_stats 
		WHERE chat_id = ? 
		GROUP BY user_id, username, first_name
		ORDER BY total DESC 
		LIMIT ?
	`, chatID, limit).Scan(&stats)

	return stats
}
