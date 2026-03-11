# CHAT_SPEC.md — EcoRouter Chat (Milestone 2)

## Что делаем

Веб-чат поверх EcoRouter API. Как OpenRouter Playground, но с встроенными тулзами (web search, файлы) и eco-брендингом.

## Архитектура

```
Browser (chat UI)
    │
    ▼
EcoRouter backend (/v1/chat/*)
    │
    ├── Если модель вернула tool_calls:
    │   ├── web_search → Brave/Tavily API → результаты → обратно в модель
    │   ├── read_file → загруженный файл из памяти → обратно в модель
    │   └── (будущее: RAG, S3 файлы, etc.)
    │
    └── Финальный ответ → стриминг в браузер
```

**Ключевое:** Тулзы выполняются на бэке, не в браузере. Юзер не видит промежуточные tool_calls — только финальный ответ. Бэк делает agentic loop: вызвал модель → получил tool_call → выполнил → отправил результат → повторить пока модель не ответит текстом.

## Что входит в MVP

### 1. Chat UI (фронт)

Одна HTML страница `web/chat.html`. Доступна по `/chat`.

**Интерфейс:**
- Поле ввода сообщения + кнопка Send
- Область сообщений (user/assistant, markdown рендеринг)
- Стриминг ответа (SSE, посимвольный вывод)
- Выбор модели (dropdown, берём список из /v1/models)
- Загрузка файлов: drag-and-drop или кнопка, поддержка картинок + текстовых файлов
- Тоглы: 🔍 Web Search (вкл/выкл)
- Авторизация: поле для eco_sk_ ключа (хранится в localStorage)
- Тёмная + светлая тема (по системной настройке, но основная — светлая как на лендинге)
- Mobile responsive

**Стек:** Vanilla JS, никаких фреймворков. Markdown рендеринг через marked.js CDN. Подсветка кода через highlight.js CDN.

**UI референс:** Чистый минималистичный чат, ближе к Claude/ChatGPT чем к Slack. Одна колонка, много воздуха.

### 2. Chat API (бэк)

#### `POST /v1/chat/agent` — умный чат с тулзами

Не путать с `/v1/chat/completions` (тупой прокси). Этот эндпоинт добавляет tool definitions и выполняет agentic loop.

**Request (от фронта):**
```json
{
  "model": "gpt-4o-mini",
  "messages": [
    {"role": "user", "content": "What's the weather in Moscow?"}
  ],
  "tools_enabled": {
    "web_search": true
  },
  "files": [
    {
      "name": "data.csv",
      "content": "base64...",
      "type": "text/csv"
    }
  ],
  "stream": true
}
```

**Серверная логика:**
1. Берём messages от юзера
2. Если есть files — добавляем содержимое в system prompt или как user message
3. Если web_search включен — добавляем tool definition `web_search` в запрос
4. Отправляем в OpenRouter через обычный прокси
5. Если ответ содержит tool_calls:
   - Выполняем каждый tool на сервере
   - Добавляем результаты в messages
   - Отправляем повторный запрос в OpenRouter
   - Повторяем до max_iterations=5 или пока модель не ответит текстом
6. Стримим финальный ответ клиенту

**Response:** Стандартный SSE стрим (OpenAI format), как /v1/chat/completions.

### 3. Встроенные тулзы

#### web_search

```json
{
  "type": "function",
  "function": {
    "name": "web_search",
    "description": "Search the web for current information. Use when the user asks about recent events, facts you're unsure about, or anything that requires up-to-date data.",
    "parameters": {
      "type": "object",
      "properties": {
        "query": {
          "type": "string",
          "description": "The search query"
        }
      },
      "required": ["query"]
    }
  }
}
```

**Реализация:** Brave Search API (бесплатный тариф — 2000 запросов/мес). Возвращаем top-5 результатов (title + snippet + url) как tool result.

#### read_file (будущее, для RAG)

Для загруженных файлов. Пока файлы идут целиком в контекст (MVP), позже — через RAG с чанкингом.

### 4. Сохранение чатов (бэк)

Чаты хранятся в SQLite. Юзер видит сайдбар со списком чатов, может переключаться, создавать новые, удалять.

#### DB Schema

```sql
CREATE TABLE chats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id),
    title TEXT NOT NULL DEFAULT 'New chat',
    messages TEXT NOT NULL DEFAULT '[]',  -- JSON array of messages
    model TEXT NOT NULL DEFAULT 'gpt-4o-mini',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

`messages` — JSON массив в OpenAI формате: `[{"role":"user","content":"..."},{"role":"assistant","content":"..."}]`

#### Chat API endpoints

**`GET /v1/chat/list`** — список чатов юзера (auth required)
```json
[
  {"id": 3, "title": "Weather in Moscow", "model": "gpt-4o-mini", "updated_at": "2026-03-10T15:30:00Z"},
  {"id": 1, "title": "Hello world", "model": "claude-sonnet-4", "updated_at": "2026-03-10T14:00:00Z"}
]
```
Сортировка: newest first по updated_at.

**`POST /v1/chat/new`** — создать пустой чат (auth required)
```json
// Request (optional)
{"model": "gpt-4o-mini"}

// Response
{"id": 4, "title": "New chat", "model": "gpt-4o-mini"}
```

**`GET /v1/chat/:id`** — получить чат с полной историей (auth required)
```json
{
  "id": 3,
  "title": "Weather in Moscow",
  "model": "gpt-4o-mini",
  "messages": [
    {"role": "user", "content": "What's the weather in Moscow?"},
    {"role": "assistant", "content": "Based on current data..."}
  ],
  "created_at": "2026-03-10T15:30:00Z"
}
```

**`DELETE /v1/chat/:id`** — удалить чат (auth required, только свой)

**`POST /v1/chat/agent`** — отправка сообщения (обновлён)

Теперь принимает `chat_id`. После ответа модели сообщения автоматически сохраняются в БД.
```json
{
  "chat_id": 3,
  "model": "gpt-4o-mini",
  "messages": [
    {"role": "user", "content": "What's the weather in Moscow?"}
  ],
  "tools_enabled": {"web_search": true},
  "stream": true
}
```

Автоматический title: после первого ответа берём первые ~50 символов юзерского сообщения как title.

#### UI: Сайдбар

```
┌──────────────────┬─────────────────────────────────────┐
│ 🌱 EcoRouter     │                                     │
│                  │                                     │
│ [+ New chat]     │   Чат-область                       │
│                  │                                     │
│ Today            │   User: What's the weather?         │
│  Weather in Mo.. │   Assistant: Based on current...    │
│  Code review     │                                     │
│                  │                                     │
│ Yesterday        │                                     │
│  Hello world     │                                     │
│                  │   ┌─────────────────────────────┐   │
│                  │   │ Type a message...        📎 │   │
│ ──────────────── │   └─────────────────────────────┘   │
│ Model: gpt-4o ▾  │   🔍 Web Search: ON                 │
│ 🔍 Web Search    │                                     │
└──────────────────┴─────────────────────────────────────┘
```

Сайдбар сворачивается на мобильном (гамбургер-меню).

---

## API для поискового провайдера

### Brave Search API
- Регистрация: https://brave.com/search/api/
- Бесплатно: 2000 запросов/мес
- Нужен API ключ (BRAVE_SEARCH_API_KEY)

### Альтернативы (если Brave не подойдёт)
- Tavily (1000 бесплатных/мес, заточен под AI)
- Serper (2500 бесплатных/мес, Google results)
- DuckDuckGo (бесплатный, но нет официального API)

---

## Структура файлов (новые)

```
internal/
├── chat/
│   ├── handler.go      # POST /v1/chat/agent — agentic loop
│   ├── tools.go        # Tool definitions + execution
│   └── search.go       # Web search implementation (Brave API)
web/
├── chat.html           # Chat UI
```

---

## Безопасность

- Agentic loop: max 5 итераций (защита от бесконечных tool_calls)
- Файлы: максимум 10MB, только текст/картинки, не исполняемые
- Web search: rate limit — max 10 поисков на запрос
- eco_sk_ ключ хранится в localStorage (не идеально, но для MVP ок)

---

## Фазы реализации

### Phase 1: Базовый чат + сохранение
1. DB миграция — таблица `chats`
2. `chat/handler.go` — POST /v1/chat/agent с agentic loop + CRUD для чатов
3. `web/chat.html` — UI со стримингом + сайдбар с историей
4. Выбор модели, markdown рендеринг

### Phase 2: Web Search
4. `chat/search.go` — Brave Search интеграция
5. `chat/tools.go` — tool definitions, execution router
6. Тогл web search в UI

### Phase 3: Файлы
7. Загрузка картинок (vision — base64 в content)
8. Загрузка текстовых файлов (в контекст)

### Phase 4 (потом): RAG + S3
9. Интеграция с S3 file storage
10. Embeddings + vector search
11. Автоматический chunking документов

---

## Что НЕ делаем в MVP

- Нет multi-user shared чатов
- Нет голосового ввода
- Нет генерации картинок
- Нет MCP протокола (tools через OpenRouter tool calling = достаточно)
- Нет code execution / sandbox

---

## Env переменные (новые)

```
BRAVE_SEARCH_API_KEY=...
```
