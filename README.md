# Recall

Self-hosted, multi-user spaced repetition web app. Like Anki, but as a single Go binary you deploy with Docker.

Uses the [FSRS](https://github.com/open-spaced-repetition/go-fsrs) algorithm for optimal review scheduling.

## Features

- **Spaced repetition** with FSRS — the same algorithm powering modern Anki
- **Full-text search** over everything stored — article bodies, flashcards, chat messages — via SQLite FTS5, accent- and case-insensitive
- **Reader view** — re-read a saved article inside Recall, with your search terms highlighted
- **Multi-user** with session-based auth and row-level data isolation
- **Web UI** — HTMX + Tailwind, no JavaScript build step
- **REST API** — full JSON API for programmatic access, checked against the router by a test
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
| `LLM_API_KEY` | — | API key for the LLM provider (enables AI chat + flashcard generation) |
| `LLM_MODEL` | `openai/gpt-oss-120b` | Instance-wide default model |
| `LLM_API_URL` | Groq chat-completions | Any OpenAI-compatible chat-completions endpoint |

### Changing the model

Providers retire models. When that happens every generation call starts failing
with a 404, and the fix should never be a rebuild. The model is resolved per
call, most specific first:

1. **The user's own setting** — the *AI Model* field on `/profile`, or
   `PATCH /api/v1/account` with `{"llm_model": "..."}`. Takes effect on the next
   request; nothing to restart.
2. **`LLM_MODEL`** — the instance default, for a fresh deploy or to move every
   user at once. Needs a container restart, not a rebuild.
3. **A compiled fallback** — last resort only.

Leave the profile field empty to follow the instance default. `GET /api/v1/account`
reports both `llm_model` (your override) and `llm_model_effective` (what a call
would actually use right now).

## Usage

1. **Register** an account at `/register`
2. **Create a deck** from the dashboard
3. **Add cards** manually or import a CSV (two columns: front, back)
4. **Study** — cards are scheduled by FSRS. Rate each card:
   - **Again** — forgot it, review soon
   - **Hard** — struggled, shorter interval
   - **Good** — remembered, normal interval
   - **Easy** — effortless, longer interval

## CLI

The binary doubles as its own admin tool. Every subcommand reads `RECALL_DB_PATH`,
so any of them can be pointed at a copy of the database instead of the live one.

```bash
recall list-users
recall reset-password <email> <new-password>
recall create-token <email> <token-name>
recall set-admin <email>
recall reindex                        # rebuild the full-text index
recall routes                         # print the API route table
recall metrics <email> [--json] [--tz <Zone>]
```

### `recall metrics`

Reports what the collection is actually doing, rather than how big it is. It is
read-only, and two runs against an unchanged database produce identical bytes, so
its output can be diffed over time or asserted on in a probe.

```bash
RECALL_DB_PATH=./copy.db recall metrics you@example.com --tz Europe/Madrid
```

It prints eight measures:

| Measure | What it tells you |
|---|---|
| **True retention** | Share of *spaced* reviews (`elapsed_days > 0`) not rated Again. First exposures are excluded — they say nothing about remembering. |
| **Rating distribution** | The four-way split, plus, for each button, how often the *next* review of that card was a failure. A middle button that predicts failure no better than Good is noise the scheduler cannot use. |
| **Leeches** | Cards at or above 8 lapses, and the highest lapse count. A card failed eight times is usually malformed rather than hard. |
| **Sibling interference** | Share of reviews landing on the same day as another card from the same article. Siblings share retrieval cues, so the first one primes the rest and the session reports a recall it did not earn. |
| **Backs with a list** | Cards whose answer is an enumeration. A five-item answer cannot be failed, only failed partially — and no rating button says that. |
| **Fronts with a conjunction** | Cards asking two things at once, matched on `y` / `e` / `and` as whole words. |
| **Cards per deck** | Whether the collection has any structure to slice by. |
| **Reviews by hour** | When studying actually happens, in the requested zone. |

Percentages are rounded to one decimal; day boundaries and hours use `--tz`
(defaulting to the system zone), so a session ending after midnight lands on the
day the learner thinks it does.

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

# Search everything you've saved
curl -H "Authorization: Bearer rcl_..." "http://localhost:8080/api/v1/search?q=canto+gregoriano"

# Read an article's stored text
curl -H "Authorization: Bearer rcl_..." http://localhost:8080/api/v1/articles/{id}/content

# Stats
curl -b cookies http://localhost:8080/api/v1/stats
```

<details>
<summary>Full API reference</summary>

Browse it live at `/docs` (Swagger UI over `static/openapi.yaml`), or print the
router with `recall routes`. A test diffs the two, so the table below and the
spec cannot drift from the code.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/health` | Liveness (no auth) |
| POST | `/api/v1/auth/register` | Register |
| POST | `/api/v1/auth/login` | Login |
| POST | `/api/v1/auth/logout` | Logout |
| GET | `/api/v1/me` | Current user + settings |
| PUT | `/api/v1/me/settings` | Update settings |
| GET | `/api/v1/me/tokens` | List API tokens |
| POST | `/api/v1/me/tokens` | Create API token |
| DELETE | `/api/v1/me/tokens/:id` | Revoke API token |
| GET | `/api/v1/search` | Full-text search (`q`, `kind`, `limit`, `offset`) |
| POST | `/api/v1/search/reindex` | Rebuild the index (admin) |
| GET | `/api/v1/articles` | List articles (no bodies) |
| POST | `/api/v1/articles` | Save an article |
| GET | `/api/v1/articles/:id` | Article **with** its stored text |
| PUT | `/api/v1/articles/:id` | Update title/text |
| DELETE | `/api/v1/articles/:id` | Delete article + its cards |
| GET | `/api/v1/articles/:id/content` | Stored text as `text/plain` |
| GET | `/api/v1/articles/:id/cards` | Cards generated from the article |
| POST | `/api/v1/articles/:id/generate` | Generate flashcards with the LLM |
| GET | `/api/v1/articles/:id/chat` | Chat history |
| POST | `/api/v1/articles/:id/chat` | Ask about the article |
| DELETE | `/api/v1/articles/:id/chat` | Clear chat |
| GET | `/api/v1/decks` | List decks |
| POST | `/api/v1/decks` | Create deck |
| GET | `/api/v1/decks/:id` | Get deck |
| PUT | `/api/v1/decks/:id` | Update deck |
| DELETE | `/api/v1/decks/:id` | Delete deck |
| GET | `/api/v1/decks/:id/cards` | List cards in a deck |
| POST | `/api/v1/decks/:id/cards` | Create card |
| POST | `/api/v1/decks/:id/import` | Import CSV |
| GET | `/api/v1/decks/:id/study` | Next due card |
| POST | `/api/v1/decks/:id/study` | Submit review |
| GET | `/api/v1/cards` | Cards across all decks (`deck_id`, `article_id`, `due`) |
| GET | `/api/v1/cards/:id` | Get card |
| PUT | `/api/v1/cards/:id` | Update card |
| DELETE | `/api/v1/cards/:id` | Delete card |
| GET | `/api/v1/playlists` | List playlists |
| POST | `/api/v1/playlists` | Add playlist |
| GET | `/api/v1/playlists/:id` | Get playlist |
| DELETE | `/api/v1/playlists/:id` | Delete playlist |
| POST | `/api/v1/playlists/:id/articles` | Link article |
| DELETE | `/api/v1/playlists/:id/articles/:articleID` | Unlink article |
| POST | `/api/v1/playlists/:id/decks` | Link deck |
| DELETE | `/api/v1/playlists/:id/decks/:deckID` | Unlink deck |
| GET | `/api/v1/podcasts` | List podcasts |
| POST | `/api/v1/podcasts` | Queue a podcast |
| GET | `/api/v1/podcasts/pending` | Production queue |
| GET | `/api/v1/podcasts/:id` | Get podcast |
| DELETE | `/api/v1/podcasts/:id` | Delete podcast |
| GET | `/api/v1/podcasts/:id/content` | Source text behind a podcast |
| PUT | `/api/v1/podcasts/:id/status` | Report production status |
| GET | `/api/v1/stats` | Card totals, due count, streak |
| GET | `/api/v1/stats/history` | Daily review counts |

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
| F1 | **AI Chat** | ✅ Done | Per-article Gemini chat with persistent history |
| F2 | **Daily Podcast** | ✅ Done | Auto-generated daily audio from recent articles via NotebookLM |
| F3 | **Configurable Flashcards** | ✅ Done | Per-user card limit (1-20/day) |
| F4 | **Playlist Manager** | ✅ Done | Spotify/YouTube playlists linked to articles |
| F5 | **Readeck Sync** | ✅ Done | Tag "recall" in Readeck → auto-imported every 15 min |
| F6 | **Custom Flashcard Prompt** | ✅ Done | Editable Gemini prompt per user in Settings. Uses `{count}` placeholder |
| F7 | **Smart Review Order** | ✅ Done | New/learning cards shown before review cards so new cards don't pile up |
| F8 | **Dark/Light Mode** | ✅ Done | Toggle with localStorage persistence |
| F9 | **Full-text search** | ✅ Done | FTS5 index over articles, flashcards and chats; live `/search` page + `GET /api/v1/search` |
| F10 | **Reader view** | ✅ Done | `/to-read/:id/read` renders the stored text with `?q=` highlighting and font-size control |
| F11 | **Complete REST API** | ✅ Done | Whole app surface exposed; OpenAPI spec diffed against the router by a test |

## Backlog

- **Spaced repetition analytics** — retention curves, optimal study times
- **Mobile PWA** — installable web app with offline study
- **Deck sharing** — public deck URLs for collaboration
- **Tag system** — organize decks and articles by tags
- **Composite DB index** — `(deck_id, state, due)` for faster review queries at scale
- **UserService abstraction** — consolidate scattered `db.QueryRow` user queries

## License

MIT
