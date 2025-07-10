[![Deploy Summary Bot](https://github.com/Viste/summarybot/actions/workflows/deploy.yml/badge.svg)](https://github.com/Viste/summarybot/actions/workflows/deploy.yml) 
# SummaryBot 🤖

Telegram бот на Go для создания резюме сообщений чата с использованием ИИ.

## Возможности

- 📋 Анализирует сообщения чата за указанный период
- 🧠 Использует OpenAI API для создания умных резюме
- 💾 Сохраняет историю сообщений в SQLite

## Команды

Упомяните бота в чате:
- `@123_bot что было за сегодня`
- `@123_bot что было за вчера`
- `@123_bot что было за позавчера`
- `@123_bot что было за 3 дня` (максимум 7 дней)

## Быстрый старт

### 1. Локальная разработка

```bash
# Клонируем репозиторий
git clone <your-repo-url>
cd summarybot

# Копируем конфигурацию
cp .env.example .env

# Редактируем .env с вашими токенами
nano .env

# Устанавливаем зависимости
go mod download

# Запускаем бота
go run main.go
```

### 2. Docker

```bash
# Сборка образа
docker build -t summarybot .

# Запуск контейнера
docker run -d \
  -e TELEGRAM_BOT_TOKEN=your_token \
  -e OPENAI_API_KEY=your_key \
  -e OPENAI_BASE_URL=http://IP:9000/v1 \
  -v $(pwd)/data:/data \
  -p 8080:8080 \
  summarybot
```

## Настройка

### Переменные окружения

| Переменная | Описание | Пример |
|------------|----------|---------|
| `TELEGRAM_BOT_TOKEN` | Токен Telegram бота | `123456:ABC-DEF...` |
| `OPENAI_API_KEY` | Ключ OpenAI API | `sk-proj-...` |
| `OPENAI_BASE_URL` | Базовый URL OpenAI | `http://IP:9000/v1` |
| `BOT_USERNAME` | Имя пользователя бота | `zagichak_bot` |
| `DATABASE_PATH` | Путь к базе SQLite | `./summarybot.db` |
| `PORT` | Порт для health-check | `8080` |

### Создание Telegram бота

1. Напишите [@BotFather](https://t.me/botfather)
2. Выполните команду `/newbot`
3. Следуйте инструкциям
4. Сохраните полученный токен

### Настройка OpenAI

Бот использует прокси OpenAI по адресу `http://IP:9000/v1`.

## Архитектура

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Telegram      │    │   SummaryBot    │    │   OpenAI API    │
│   Chat          │◄──►│   (Go)          │◄──►│   (via proxy)   │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │
                                ▼
                        ┌─────────────────┐
                        │   SQLite DB     │
                        │   (Messages &   │
                        │   Summaries)    │
                        └─────────────────┘
```

### Компоненты

- **main.go** - Основная логика бота
- **SQLite** - Локальное хранение сообщений и резюме
- **OpenAI API** - Генерация резюме через ИИ
- **Telegram API** - Взаимодействие с пользователями

## Мониторинг

### Health Check

```bash
curl http://localhost:8080/healthz
```

### Логи

```bash
# Kubernetes
kubectl logs -f deployment/summarybot -n summarybot

# Docker
docker logs -f summarybot
```

## FAQ

**Q: Бот не отвечает на сообщения?**
A: Убедитесь, что бот добавлен в чат и имеет права на чтение сообщений.

**Q: Ошибка "OpenAI API недоступен"?**
A: Проверьте доступность прокси-сервера и правильность API ключа.

**Q: База данных не сохраняется в Kubernetes?**
A: Убедитесь, что PVC создан и примонтирован корректно.

## Лицензия

MIT License
