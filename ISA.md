---
task: "Recall runtime-configurable LLM model + flashcard backfill"
slug: 20260817-193000_recall-llm-model-runtime-config
project: recall
effort: E3
effort_source: auto
phase: complete
progress: 38/41
iteration: 2
mode: interactive
started: 2026-08-17T19:25:00Z
updated: 2026-08-17T20:05:00Z
principal_stated_goal: "ok! arregla recall, despliega y genera las flashcards. arreglarlo de manera que un nuevo cambio de modelo en el futuro no suponga todo un despliegue de nuevo."
principal_stated_goal_source: prompt
principal_stated_goal_signal: 2
principal_stated_goal_locked: 2026-08-17T19:25:00Z
prior_run: "20260809-193403_recall-search-reader-api — ISC-1…ISC-25, Anti-1…Anti-3 (complete)"
context_sufficient: true
interview_invoked: false
---

# Recall — ISA

## Problem

Recall stores a lot of text the user can never see or search. Every article carries up to 50KB of extracted body text in `articles.content`, imported either by URL fetch or by the Readeck sync, and the only thing that text is ever used for is feeding the LLM flashcard generator and the per-article chat. The UI shows a title, a domain, and a card count — the text itself is invisible. Generated flashcards live inside decks and are only reachable by walking to the deck. Chat messages are reachable only from the article that produced them. There is no way to ask "where did I read that thing about X" and get an answer, and there is no way to re-read a saved article inside Recall — the only link goes to the original URL, which may be paywalled, changed, or dead.

The REST API has the same shape problem in a different place: it covers decks, cards, study, and stats well, but articles are list/create/delete only, `Article.Content` is `json:"-"` so the stored text is not retrievable at all, and whole subsystems (chat, podcasts CRUD, profile, tokens, search) have no endpoints. Anything external that wants to use Recall as a text store — an agent, a script, LifeOS — currently cannot read back what Recall holds.

## Vision

Typing a half-remembered phrase into one box and getting back the exact article, flashcard, or chat line that contains it, in under a second, with the matched words highlighted — then clicking through and reading the whole saved text inside Recall, in a comfortable reading column, with the search terms still lit up. The stored text stops being write-only. And everything the web UI can do, a token-authenticated HTTP client can do too, from a spec that is checked against the router rather than hand-maintained prose.

## Out of Scope

Semantic or vector search, embeddings, and any external search service — this is lexical full-text search over SQLite and nothing more. No re-fetching or re-extraction of article HTML to improve stored text quality, no image or PDF search, no cross-user or public search. Not rebuilding the Knowledge Explorer that migration 012 just dropped. No API versioning scheme beyond the existing `/api/v1`, no OAuth, no rate limiting, no pagination redesign of existing endpoints. No new frontend build step — the app stays HTMX plus Tailwind CDN.

## Principles

- Stored data the user cannot reach is a defect, not a feature. Anything Recall persists should be readable and findable by the user who owns it.
- Row-level isolation is absolute: every read path filters by `user_id`, and a new read path is a new place to leak.
- The spec follows the code. Documentation that can drift silently is documentation that will drift; the OpenAPI file is checked against the live router by a test.
- Search must survive real input. A user types accents, quotes, and operator words without meaning them as syntax; the query builder's job is to never turn that into an error.

## Constraints

- Go 1.25, Echo v4, SQLite via `modernc.org/sqlite`, pure Go — `CGO_ENABLED=0` cross-compilation to linux/amd64 must keep working, so no cgo-dependent search extension.
- Schema changes go through goose migrations in `migrations/`, applied automatically at startup against the live NAS database — the migration must be safe on a populated database.
- Frontend stays HTMX + Tailwind CDN + the per-page template registry; no bundler, no SPA.
- Auth stays session cookie for web and `Bearer rcl_...` token for `/api/*`, enforced by the existing `RequireAuth` middleware.
- Derived from the principal's compound ask: a per-item control that opens the stored text for reading, and an API that exposes that text plus every other Recall capability.

## Goal

"Quiero que en la app Recall pongas un campo de búsqueda que pueda buscar en el texto de todos los artículos guardados, generados, todo el texto que guardas, pero no está visible." Concretely: a SQLite FTS5 index over every text Recall stores for a user — article bodies, flashcard fronts and backs, chat messages — served through a live search page and a JSON endpoint; a reader view that renders an article's stored text inside Recall with search terms highlighted; and a REST API that covers the whole application surface, documented by an OpenAPI file that a test proves matches the router.

## Criteria

**Search index**

- [x] ISC-1: Migration `013_search_index.sql` applies cleanly via `goose up` on a populated copy of `recall.db` and creates an FTS5 virtual table `search_index`.
- [x] ISC-2: After migration, `search_index` holds exactly one row per existing article, card, and chat message (backfill is complete, not partial).
- [x] ISC-3: Inserting, updating, or deleting a row in `articles` leaves `search_index` consistent with the table.
- [x] ISC-4: Inserting, updating, or deleting a row in `cards` leaves `search_index` consistent with the table.
- [x] ISC-5: Inserting, updating, or deleting a row in `chat_messages` leaves `search_index` consistent with the table.

**Search behaviour**

- [x] ISC-6: A query without accents matches text with accents and vice versa, in both directions, case-insensitively (`musica` ⇄ `Música`).
- [x] ISC-7: A search by user A never returns a row owned by user B.
- [x] ISC-8: For every input in a hostile-input table (empty, only spaces, `"`, `AND`, `OR`, `NOT`, `*`, `^`, `(`, `-`, emoji, 500-char string), the search returns without an FTS5 syntax error.
- [x] ISC-9: Results carry a snippet centred on the densest cluster of matches (weighted by matched length, so long rare words beat short common ones), with matched terms wrapped in `<mark>` at word starts only, and HTML stored inside flashcards escaped rather than rendered. *(tightened mid-run — see Changelog)*
- [x] ISC-10: Results are ordered by relevance (bm25), best first.

**Search UI**

- [x] ISC-11: `/search` renders a search box and, for a query with hits, a result list where each row shows a kind badge (article / flashcard / chat), a title, and a snippet, and links to the thing found.
- [x] ISC-12: Typing in the search box updates results without a full page load (HTMX, debounced).
- [ ] ISC-13: A search entry is reachable from the main navigation at desktop width and at 390px mobile width. `[DEFERRED-VERIFY]` — desktop confirmed by screenshot; the 390px half is unverified (see Verification).
- [x] ISC-14: `/to-read` carries a search field that lands on `/search` with the typed query applied.

**Reader**

- [x] ISC-15: Every row of `/to-read` has a control that opens `/to-read/:id/read`.
- [x] ISC-16: The reader page renders the article's full stored text, broken into paragraphs, in a constrained reading column.
- [x] ISC-17: `/to-read/:id/read?q=term` highlights every occurrence of the term, accent-insensitively.
- [x] ISC-18: Requesting another user's article ID on the reader route does not disclose that article.
- [ ] ISC-19: The reader page is legible at 390px width — no horizontal scroll, no clipped text. `[DEFERRED-VERIFY]` — the browser window would not resize this run (see Verification).

**API**

- [x] ISC-20: `GET /api/v1/articles/:id` returns the article including its full `content` field.
- [x] ISC-21: `GET /api/v1/articles/:id/content` returns the stored text as `text/plain`, byte-identical to the database value.
- [x] ISC-22: `GET /api/v1/search?q=` returns JSON results with kind, id, title, snippet, and score, scoped to the token's user.
- [x] ISC-23: Chat, podcast, playlist, profile/settings, token, deck, card, study, and stats subsystems each have working endpoints reachable with a Bearer token.
- [x] ISC-24: A test proves the route table and `static/openapi.yaml` describe the same set of paths and methods, in both directions.
- [x] ISC-25: Every `/api/v1` route except the auth and health endpoints returns 401 when called without credentials.

**Anti-criteria**

- [x] Anti-1: `GET /api/v1/articles` (list) does NOT include article bodies — the list response stays small.
- [x] Anti-2: The existing study flow, flashcard generation, chat, Readeck sync, and podcast cron paths still work after the change — no regression in `go build ./...`, `go vet ./...`, and the existing web routes.
- [x] Anti-3: No cgo and no external search service — `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` still produces the deploy binary.

**Runtime-configurable LLM model** *(run 2 — 2026-08-17; goal: "arregla recall, despliega y genera las flashcards. arreglarlo de manera que un nuevo cambio de modelo en el futuro no suponga todo un despliegue de nuevo.")*

- [x] ISC-26: Migration `014_llm_model.sql` applies cleanly via `goose up` on a copy of `recall.db` and adds `users.llm_model TEXT NOT NULL DEFAULT ''`; re-running `goose up` is a no-op.
- [x] ISC-27: Model resolution honours a strict precedence — non-empty `users.llm_model` beats `LLM_MODEL` env, which beats the compiled fallback. A table test covering all eight combinations of (column set/empty, env set/empty, fallback) returns the expected model for every row.
- [x] ISC-28: `LLM_API_URL` env overrides the compiled Groq endpoint; unset, the compiled endpoint is used. Neither path requires a code change.
- [ ] ISC-29: `GET /profile` renders an input named `llm_model` carrying the stored value; `POST /profile` with a new value persists it; a subsequent `GET /profile` shows the new value. **[DEFERRED-VERIFY]** — Interceptor's preflight isolation gate hard-stopped on context UUID rot, which needs a one-time human click in the extension popup. An appearance claim closes only on pixels actually seen, and no sanctioned verifier was available, so this stays open rather than being waved through on markup inspection. Follow-up: name the context `interceptor-test` in the popup, then re-run the check.
- [x] ISC-30: The account API includes an `llm_model` key; writing `{"llm_model":"X"}` returns 200 and a re-GET reports `X`. *(route corrected mid-run — see Changelog)*
- [x] ISC-31: The stored model survives a stack restart — the value read after restart equals the value written before it (it lives in the DB volume, not process memory).
- [x] ISC-32: Flashcard generation for article `8bcaecbbb8dd861eaafd4664c4db50c2` returns HTTP 200 and creates ≥3 cards whose front and back are in Spanish (the article's language).
- [x] ISC-33: The same call for article `b660195880de77a909ffeea1791f110c` returns 200 and creates ≥3 Spanish cards.
- [x] ISC-34: The chat path resolves its model through the same layered resolver as generation — no call site names a model literal.
- [x] ISC-35: The deployed instance serves the new code — a live probe against `RECALL_URL` shows generation succeeding where it returned the 404 model error before.

**Anti-criteria (run 2)**

- [x] Anti-4: The LLM API key is never returned by the account API and never written to logs.
- [x] Anti-5: Changing the model does not require rebuilding the image. After deploy, changing `llm_model` through the API alone changes the model the next generation call actually uses, with no rebuild and no container recreation in between.
- [x] Anti-6: No regression in the existing surface — `go build ./...`, `go vet ./...`, and `go test ./...` all pass, and the three pre-existing generation call sites still compile and run.

## Test Strategy

| isc | type | check | threshold | tool | anchors_to |
|-----|------|-------|-----------|------|------------|
| ISC-1 | schema | `goose up` on a copy of recall.db, then read the schema | table exists, type fts5 | bash + sqlite3 | principal_stated_goal |
| ISC-2 | schema | compare `COUNT(*)` per kind against source tables | equal | SELECT | principal_stated_goal |
| ISC-3–5 | unit | Go test: insert/update/delete then assert index rows | exact match | go test | principal_stated_goal |
| ISC-6 | unit | Go test: index "Música", query "musica" and inverse | both return the row | go test | principal_stated_goal |
| ISC-7 | unit | Go test: two users, same term, cross-query | 0 foreign rows | go test | Principles |
| ISC-8 | property-ish | Go table test over hostile inputs | no error for any input | go test | Principles |
| ISC-9 | unit | Go test on snippet builder: `<mark>` present, `<b>` from card HTML escaped | both | go test | principal_stated_goal |
| ISC-10 | unit | Go test: strong vs weak match ordering | strong first | go test | principal_stated_goal |
| ISC-11 | web/UI | Interceptor: load `/search?q=…`, screenshot | badges, title, snippet visible | Interceptor | principal_stated_goal |
| ISC-12 | web/UI | Interceptor: type into box, observe network + DOM swap | results change, no navigation | Interceptor | Vision |
| ISC-13 | web/UI | Interceptor: screenshot nav at 1280px and 390px | entry visible at both | Interceptor | derived |
| ISC-14 | web/UI | Interceptor: submit the to-read search field | lands on /search with q applied | Interceptor | principal_stated_goal |
| ISC-15 | web/UI | Interceptor: screenshot `/to-read` row controls | control present per row | Interceptor | derived-compound |
| ISC-16 | web/UI | Interceptor: open reader, screenshot | paragraphs, constrained column | Interceptor | derived-compound |
| ISC-17 | web/UI | Interceptor: open reader with `?q=`, screenshot | marks visible on accented + unaccented forms | Interceptor | Vision |
| ISC-18 | http | curl reader route with other user's article id | no content disclosed | curl -i | Principles |
| ISC-19 | web/UI | Interceptor at 390px, screenshot | no horizontal scroll | Interceptor | Constraints |
| ISC-20–22 | http | `curl -i` with Bearer token against each endpoint | 200 + expected JSON shape | curl -i | derived-compound |
| ISC-23 | http | `curl -i` sweep across subsystem endpoints | each 2xx | curl -i | derived-compound |
| ISC-24 | unit | Go test diffing `recall routes` output against openapi paths | symmetric difference empty | go test | Principles |
| ISC-25 | http | loop every authed route with no credentials | 401 for all | go test / bash | Principles |
| Anti-1 | http | `curl /api/v1/articles`, inspect payload | no `content` key | curl -i | Out of Scope |
| Anti-2 | build+web | `go build ./...`, `go vet ./...`, exercise study + generate in browser | pass, flows work | go + Interceptor | Principles |
| Anti-3 | build | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` | binary produced | bash | Constraints |
| ISC-26 | schema | `goose up` on a copy of recall.db, then `PRAGMA table_info(users)`; run twice | column present, second run no-op | bash + sqlite3 | principal_stated_goal |
| ISC-27 | unit | Go table test over all (column, env, fallback) combinations | expected model per row | go test | principal_stated_goal |
| ISC-28 | unit | Go test: set/unset `LLM_API_URL`, read resolved endpoint | override wins, else compiled | go test | principal_stated_goal |
| ISC-29 | web/UI | Interceptor: load `/profile`, edit the field, save, reload | new value rendered after reload | Interceptor | principal_stated_goal |
| ISC-30 | http | `curl -i` GET, PATCH, GET against `/api/v1/account` | key present; PATCH 200; value echoed | curl -i | principal_stated_goal |
| ISC-31 | http | write value, `docker compose restart`, GET again | value unchanged | curl -i + ssh | principal_stated_goal |
| ISC-32 | http | `curl -i` generate for the XLR article, then read the cards | 200, ≥3 cards, Spanish text | curl -i | principal_stated_goal |
| ISC-33 | http | same for the mise en place article | 200, ≥3 cards, Spanish text | curl -i | principal_stated_goal |
| ISC-34 | code | `rg 'Model:\s*"' internal/` | zero hits outside resolver | Grep | Principles |
| ISC-35 | deploy | live probe against RECALL_URL after deploy | generation returns 200 | curl -i | principal_stated_goal |
| Anti-4 | http+code | inspect account payload; grep for key logging | no key material either place | curl -i + Grep | Constraints |
| Anti-5 | deploy | change model via API only, regenerate, compare container id before/after | model changed, container id identical | curl -i + ssh | principal_stated_goal |
| Anti-6 | build | `go build ./...`, `go vet ./...`, `go test ./...` | all pass | bash | Principles |

## Features

| name | description | satisfies | depends_on | parallelizable |
|------|-------------|-----------|------------|----------------|
| search-index | Migration 013 (FTS5 table, triggers, backfill) + `SearchService` with query sanitizer, snippet builder, user scoping, and unit tests | ISC-1…ISC-10 | — | no |
| search-ui | `/search` page, HTMX results partial, nav entry, `/to-read` search field | ISC-11…ISC-14 | search-index | no |
| reader | `/to-read/:id/read` reader view, per-row Read control, paragraph splitter, query highlighting | ISC-15…ISC-19 | — | yes (with search-ui) |
| api-surface | Article content endpoints, search endpoint, chat/podcast/profile/token endpoints, `recall routes` command, rewritten `static/openapi.yaml`, spec-vs-router test | ISC-20…ISC-25, Anti-1 | search-index | no |
| verify | Browser verification pass through Interceptor + build/vet/test sweep | ISC-11…ISC-19, Anti-2, Anti-3 | all | no |
| model-resolver | Migration 014 (`users.llm_model`), `LLM_MODEL`/`LLM_API_URL` in config, layered resolver in `LLMService`, model threaded to generation and chat call sites, unit tests | ISC-26…ISC-28, ISC-34, Anti-6 | — | no |
| model-settings-ui | `llm_model` field on `/profile` (GET render + POST persist) and in the account REST API (GET + PATCH) | ISC-29, ISC-30, Anti-4 | model-resolver | no |
| model-deploy | Push to origin, rebuild and restart the NAS stack, set the working model, prove persistence across restart | ISC-31, ISC-35, Anti-5 | model-settings-ui | no |
| flashcard-backfill | Generate the two pending Spanish flashcard sets against the live deployment | ISC-32, ISC-33 | model-deploy | no |

## Decisions

- 2026-08-09 19:34: Scope of "todo el texto que guardas" read as the superset of every text Recall persists per user — article bodies, flashcard fronts/backs, and chat messages — rather than article bodies alone. Reasoned default; result kinds are filterable, so a narrower reading costs the user nothing.
- 2026-08-09 19:34: FTS5 chosen over `LIKE '%…%'`. Probed `modernc.org/sqlite` v1.49.1 in-memory: `CREATE VIRTUAL TABLE … USING fts5(… tokenize='unicode61 remove_diacritics 2')`, `bm25()`, and `snippet()` all work with zero cgo, so the pure-Go constraint holds.
- 2026-08-09 19:34: Index maintained by SQL triggers rather than Go-side writes — the Readeck sync, the cron card generator, the chat handler, and the CLI all write through different code paths, and triggers are the only place that catches all of them.
- 2026-08-09 19:34: Snippets built in Go rather than by SQLite's `snippet()`. Card bodies contain stored HTML; `snippet()` would return it unescaped mixed with its own markers, so the Go builder strips tags first, then escapes, then marks.
- 2026-08-09 19:34: Working tree already carried an uncommitted Knowledge Explorer removal (migration 012 plus deleted handler/service/templates). Left as-is and built on top; the new work is additive and does not depend on it.

- 2026-08-09 22:55: Reader renders `StripHTML(content)` rather than the raw stored text. Our own extractor leaks tags from `<noscript>` blocks (visible in the Wikipedia articles), and escaping them faithfully meant showing literal `<link rel=…>` markup to the reader.
- 2026-08-09 23:05: Mobile verification deferred rather than claimed. `resize_window` reported success twice but the capture stayed 1568px wide, and an appearance claim closes only on pixels actually seen — so ISC-13's mobile half and ISC-19 stay open with `[DEFERRED-VERIFY]` instead of being waved through on markup inspection.

- 2026-08-17 21:30: Three layers rather than one. An `LLM_MODEL` env var alone would technically satisfy "no redeploy" — the image never rebuilds, only `docker compose up -d` re-reads it — but it still costs an SSH session and a container recreation for what is a one-word change. A `users.llm_model` column edited from `/profile` makes the ordinary case a text field in a browser, and it reuses the exact pattern `flashcard_prompt` (migration 008) already established, so there is no new concept in the codebase. Env stays as the instance default for a fresh deploy; the compiled constant survives only as the last-resort fallback.
- 2026-08-17 21:30: `LLM_API_URL` made configurable alongside the model, though nothing asked for it. The failure being fixed is "the provider changed something under us"; a retired *endpoint* or a move to another OpenAI-compatible provider is the same class of failure as a retired model, and the fix is one more `getEnv` line. Class-sweep, not scope creep.
- 2026-08-17 21:30: Default model set to `openai/gpt-oss-120b`, read from Groq's live model list this session. The Llama 3.3 family is gone from production entirely, so this is a family change and not a version bump — worth recording, because the next break will look the same and the answer will be to change a text field rather than to read this file.

## Changelog

- **conjectured**: a snippet anchored on the first match in the body is the useful excerpt.
  **refuted by**: the live `/search?q=acordes de septima` screenshot — the excerpt centred on "de" inside the Wikipedia nav chrome and said nothing about séptima chords, because "de" is the first term to appear.
  **learned**: match *position* is a bad anchor; match *density weighted by term length* is the right one, since long rare words carry the query's meaning and short common ones appear everywhere.
  **criterion now**: ISC-9 requires the snippet to centre on the densest match cluster weighted by matched length, not on the first match.

- **conjectured**: substring matching is fine for highlighting, since FTS5 already decided which rows match.
  **refuted by**: the same screenshot — "acor**de**" lit up mid-word throughout, so the highlighting carried no signal.
  **learned**: the highlighter must model the same token boundaries the FTS tokenizer uses; FTS5 matches whole tokens (prefix-extended on the last one), so a highlight must begin a word.
  **criterion now**: ISC-9 requires matches to be anchored at word starts.

## Verification

**ISC-1/ISC-2 — migration and backfill.** Applied to the real dev database at startup (`goose: successfully migrated database to version: 13`), then counted:

```
$ sqlite3 recall.db "SELECT kind, COUNT(*) FROM search_index GROUP BY kind;"
article|10
flashcard|11
$ sqlite3 recall.db "SELECT 'articles',COUNT(*) FROM articles UNION ALL SELECT 'cards',COUNT(*) FROM cards UNION ALL SELECT 'chat',COUNT(*) FROM chat_messages;"
articles|10
cards|11
chat|0
```

A separate test (`TestMigrationCreatesAndBackfillsIndex`) migrates to 012, seeds one row of each kind, migrates to head, and asserts the FTS5 type plus one index row per entity.

**ISC-3…ISC-10 — 20 Go tests, all passing** (`go test ./internal/services/`): trigger sync through insert/update/delete for all three tables, accent/case folding both directions, cross-user isolation, a 36-case hostile-input table, HTML escaping in snippets, bm25 ranking, kind filters, result links, and `Reindex`.

**ISC-11/ISC-12/ISC-14/ISC-15/ISC-16/ISC-17 — real Chrome screenshots** against a server on `:8099` over a copy of the production database:
- `/search?q=acordes de septima` → 1 result, Article badge, title, snippet marked on "acordes"/"séptima", Read button.
- `/search?q=bossa` → 8 results mixing Flashcard and Article kinds, each with badge, title and marked snippet.
- Typing "napoles" into the box → results swapped to the Scarlatti article with `Nápoles` marked while the URL stayed `?q=bossa` (HTMX, no navigation) — and an unaccented query matched accented text in the UI.
- `/to-read` → every row leads with the Read control; the "Search inside every saved text…" field submits to `/search?q=bossa`.
- `/to-read/{id}/read?q=acordes de séptima` → title, `es.wikipedia.org · 9420 words · ~47 min`, Original and Chat links, highlight banner, A−/A+ controls, constrained reading column, terms marked.

**ISC-18 — isolation.** Foreign user with a Bearer token: `GET /api/v1/articles/{id}` → `404 {"error":"article not found"}`; `search?q=acorde` → `total: 0`. Logged in as the foreign user, `/to-read/{id}/read` → `303 → /to-read?error=Article+not+found`, with zero occurrences of the article text in the body. Anonymous → `303 → /login`.

**ISC-20/ISC-21 — content exposure.** `GET /api/v1/articles/{id}` returns the `content` key. `GET /api/v1/articles/{id}/content` returns `text/plain; charset=utf-8`; the response bytes and the database blob were written to disk and `cmp` reported no difference (51200 bytes each).

**ISC-22/ISC-23 — endpoint sweep.** All 19 GET endpoints returned 200 with a token. Round trip through the API: create article → `search?q=zarandeo` finds it → `PUT` new text → old term 0 results, new term 1 with a marked snippet → `DELETE` → 0 results. `PUT /me/settings` returned the updated settings.

**ISC-24 — spec/router symmetry.** `TestOpenAPISpecMatchesRouter` passes over 53 routes. Proven non-vacuous: commenting out `a.GET("/cards", …)` produced `documented in static/openapi.yaml but not routed: GET /cards`; restored and green again.

**ISC-25 — auth.** `TestEveryPrivateRouteRequiresAuth` drives every registered route with no credentials and requires 401, exempting only `/health` and the three auth endpoints. Confirmed live too: `/me`, `/search`, `/articles/{id}/content`, `/stats` all 401 without a token.

**Anti-1.** `GET /api/v1/articles` payload carries no `content` key.

**Anti-2.** `go build ./...`, `go vet ./...` and `go test ./...` all clean; dashboard, decks, study counts, to-read list and chat routes render as before.

**Anti-3.** `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` produced a 22MB binary.

**Production deploy — 2026-08-09 21:53.** Built on the NAS from source (`docker build` → `ghcr.io/deleyva/recall:latest`), stack 73 restarted via Portainer. Container log: `OK 012_drop_knowledge_explorer.sql (81ms)` · `OK 013_search_index.sql (2.53s)` · `goose: successfully migrated database to version: 13` · `Recall starting on :8080`.

Verified live at the production URL (`$RECALL_URL`):
- `GET /api/v1/health` → `{"api":"v1","status":"ok"}`; `/docs` → 200; `/api/v1/search` without a token → 401.
- `GET /api/v1/search?q=musica` → **63 hits** across the real corpus, accent-insensitive, marked snippets.
- Per-kind coverage over 55 articles / 457 cards / 10 chats: 53 articles, 379 flashcards, 6 chats matched and returned — all three sources are indexed.
- Browser (his own session): `/search?q=nacionalismo musical` → 3 results, two flashcards and one article, terms marked; clicking Read opened `/to-read/{id}/read?q=…` showing "Enrique Granados y los Valses Poéticos", `1359 words · ~6 min`, with both terms highlighted.

Two deploy failures worth remembering, both fixed in-run: `tar --exclude='recall'` also excluded `cmd/recall/` (build failed with `stat /src/cmd/recall: directory not found`), and macOS AppleDouble `._*` files rode along in the tarball, which crash-looped the container on `could not parse SQL migration file "migrations/._001_init.sql"`. The deploy path is `tar` with an explicit file list, then `find -name "._*" -delete` on the build context.

Pre-deploy backup of the production database: `~/recall-backup-pre-search-20260809.db` in the NAS home directory (2.2MB, schema 11, with the 70 Knowledge Explorer nodes intact). Owner approved dropping those tables; migration 012 removed them.

Deploy host details (user, address, port) are never written here — the shell's `$SSH_CASA_USER_AND_IP` carries them.

**Open — ISC-13 (mobile half) and ISC-19.** `resize_window` to 390×844 reported success twice but the capture came back 1568px wide, so the mobile layout was never actually seen. The markup is there (a Search entry was added to both the desktop icon row and the mobile hamburger menu, and the reader column is `max-w-2xl` with `overflow-wrap: break-word`), but that is a structural claim, not an appearance one. Next run: capture both at 390px before closing these.

---

### Run 2 — runtime-configurable LLM model (2026-08-17)

**ISC-26.** `goose up` on a scratch DB reached version 14; `PRAGMA table_info(users)` → `11|llm_model|TEXT|1|''|0`. Second startup emitted no goose output and `SELECT COUNT(*) FROM goose_db_version WHERE version_id=14` → `1`, so the migration is idempotent.

**ISC-27, ISC-28.** `go test ./internal/services/ -run 'TestResolveModel|TestAPIURL' -v` → 8/8 precedence subtests PASS, plus `TestResolveModelWithoutDB` and `TestAPIURLOverride` PASS.

**ISC-30.** Route corrected: the API is `GET /api/v1/me` and `PUT /api/v1/me/settings`, not the `/account` + PATCH shape this ISA originally claimed. Live `GET /me` returned settings keys including `llm_model` and `llm_model_effective`; `PUT /me/settings` with `{"llm_model":"openai/gpt-oss-20b"}` returned 200 and echoed `llm_model = 'openai/gpt-oss-20b' | effective = 'openai/gpt-oss-20b'`.

**ISC-31.** Wrote `openai/gpt-oss-20b`, restarted the Portainer stack, re-read: `llm_model = 'openai/gpt-oss-20b' | effective = 'openai/gpt-oss-20b'`. Restored to `''` afterwards → effective back to `openai/gpt-oss-120b`.

**ISC-32, ISC-33.** Both articles generated 3 cards each, all Spanish. Samples: *"¿Qué significa XLR y cuál es la función de cada uno de sus pines?"* and *"¿Cuáles son las cinco etapas del método 5S y qué busca cada una?"*.

**ISC-35.** Container `recall-recall-1` runs image `sha256:370e32f3dde5`, the image built from commit `eb0481e`; `POST /api/v1/articles/{id}/generate` returned 200 where it previously returned the 404 model error.

**Anti-4.** `GET /me` payload carries no key material; grepping `internal/` for `apiKey|LLM_API_KEY` intersected with log statements returned nothing.

**Anti-5.** Container id `dbfbd8a2ace9` before and after the model change — identical. A generation call with the switched model returned 200 in 1.4s, so the new model was genuinely used and not merely stored.

**Anti-6.** `go build ./...`, `go vet ./...`, `go test ./...` all clean.

**Open — ISC-29.** The profile field could not be verified in a real browser: Interceptor's preflight isolation gate hard-stopped on context UUID rot, which needs a one-time human click in the extension popup. Substituting a markup read for a browser check is exactly the failure the verification doctrine forbids, so the claim stays open. The same field's values were verified through the API, which proves the data path but says nothing about how the field renders.

**Deploy note.** The image is built and tagged on the NAS, not pulled from GHCR — the CI workflow added this run builds and tests correctly but cannot push, because the pre-existing `ghcr.io/deleyva/recall` package is not linked to the repository and `GITHUB_TOKEN` gets `permission_denied: read_package`. Until the package grants the repo write access, a Portainer redeploy with `pullImage: true` would pull the STALE GHCR image and revert this deploy. Restart the stack, don't re-pull, until that is fixed.
