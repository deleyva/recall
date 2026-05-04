# Recall

Self-hosted, multi-user spaced repetition web app. Like Anki, but as a single Go binary you deploy with Docker.

Uses the [FSRS](https://github.com/open-spaced-repetition/go-fsrs) algorithm for optimal review scheduling.

## Features

- **Spaced repetition** with FSRS — the same algorithm powering modern Anki
- **Multi-user** with session-based auth and row-level data isolation
- **Web UI** — HTMX + Tailwind, no JavaScript build step
- **REST API** — full JSON API for programmatic access
- **CSV import** — bulk-create cards from CSV/TSV files
- **Dashboard & stats** — due counts, review streak, daily history
- **Single binary** — SQLite database, no external dependencies
- **Docker ready** — one container, DB file in a volume

## Quick Start

### Docker (recommended)

```bash
# Clone and run
git clone https://github.com/deleyva/recall.git
cd recall
docker compose up --build

# Visit http://localhost:8080
```

Set a secure session key:

```bash
RECALL_SESSION_KEY=$(openssl rand -hex 32) docker compose up --build
```

### From source

Requires Go 1.22+.

```bash
git clone https://github.com/deleyva/recall.git
cd recall
go build -o recall ./cmd/recall/
./recall
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `RECALL_PORT` | `8080` | HTTP port |
| `RECALL_DB_PATH` | `recall.db` | SQLite database file path |
| `RECALL_SESSION_KEY` | (insecure default) | 32+ char secret for session cookies |

## Usage

1. **Register** an account at `/register`
2. **Create a deck** from the dashboard
3. **Add cards** manually or import a CSV (two columns: front, back)
4. **Study** — cards are scheduled by FSRS. Rate each card:
   - **Again** — forgot it, review soon
   - **Hard** — struggled, shorter interval
   - **Good** — remembered, normal interval
   - **Easy** — effortless, longer interval

## API

All endpoints require authentication via session cookie.

```bash
# Register
curl -c cookies -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"yourpassword"}'

# Create a deck
curl -b cookies -X POST http://localhost:8080/api/v1/decks \
  -H "Content-Type: application/json" \
  -d '{"name":"My Deck","description":"..."}'

# Add a card
curl -b cookies -X POST http://localhost:8080/api/v1/decks/{deck_id}/cards \
  -H "Content-Type: application/json" \
  -d '{"front":"Question","back":"Answer"}'

# Study
curl -b cookies http://localhost:8080/api/v1/decks/{deck_id}/study

# Submit review (rating: 1=Again, 2=Hard, 3=Good, 4=Easy)
curl -b cookies -X POST http://localhost:8080/api/v1/decks/{deck_id}/study \
  -H "Content-Type: application/json" \
  -d '{"card_id":"...","rating":3}'

# Import CSV
curl -b cookies -X POST http://localhost:8080/api/v1/decks/{deck_id}/import \
  -F "file=@cards.csv"

# Stats
curl -b cookies http://localhost:8080/api/v1/stats
```

<details>
<summary>Full API reference</summary>

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/auth/register` | Register |
| POST | `/api/v1/auth/login` | Login |
| POST | `/api/v1/auth/logout` | Logout |
| GET | `/api/v1/decks` | List decks |
| POST | `/api/v1/decks` | Create deck |
| GET | `/api/v1/decks/:id` | Get deck |
| PUT | `/api/v1/decks/:id` | Update deck |
| DELETE | `/api/v1/decks/:id` | Delete deck |
| GET | `/api/v1/decks/:id/cards` | List cards |
| POST | `/api/v1/decks/:id/cards` | Create card |
| GET | `/api/v1/cards/:id` | Get card |
| PUT | `/api/v1/cards/:id` | Update card |
| DELETE | `/api/v1/cards/:id` | Delete card |
| GET | `/api/v1/decks/:id/study` | Get next due card |
| POST | `/api/v1/decks/:id/study` | Submit review |
| POST | `/api/v1/decks/:id/import` | Import CSV |
| GET | `/api/v1/stats` | Get stats |
| GET | `/api/v1/stats/history` | Review history |

</details>

## Tech Stack

- **Go** with [Echo](https://echo.labstack.com/) v4
- **SQLite** (WAL mode) via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — pure Go, no CGO
- **FSRS** via [go-fsrs](https://github.com/open-spaced-repetition/go-fsrs)
- **HTMX** + **Tailwind CSS** (CDN) for the web UI
- **Goose** for database migrations
- **gorilla/sessions** for auth

## Roadmap

| # | Feature | Status | Description |
|---|---------|--------|-------------|
| F1 | **AI Chat** | ✅ Done | Per-article chat with Gemini AI. Persistent history, HTML-sanitized responses, multi-turn conversation with article context |
| F2 | **Daily Podcast via NotebookLM** | 📋 Planned | Auto-generated daily audio overview from recent/pending articles |
| F3 | **Configurable Daily Flashcards** | ✅ Done | Per-user configurable card limit (default 5/day) |
| F4 | **Playlist Manager** | 📋 Planned | Link Spotify/YouTube playlists to articles or decks as study material |
| F5 | **Readeck → Recall Sync** | ✅ Done | Tag article with "recall" in Readeck → auto-imported every 15 min (configure in Profile) |
| F6 | **Roadmap in README** | ✅ Done | This section — updated each dev session |

## Backlog

- **AI essay questions** — daily Socratic questions generated from articles, with AI evaluation
- **Reading progress tracking** — mark articles as read/in-progress
- **Spaced repetition analytics** — retention curves, optimal study times
- **Mobile PWA** — installable web app with offline study
- **Deck sharing** — public deck URLs for collaboration
- **Markdown cards** — rich formatting in card front/back
- **Tag system** — organize decks and articles by tags

## License

MIT
