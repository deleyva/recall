---
task: "Close the gap between Recall and Anki-grade memory fidelity"
slug: 20260820-143000_recall-memory-fidelity
project: recall
effort: E4
effort_source: gate-floor
phase: build
progress: 73/119
iteration: 3
mode: interactive
started: 2026-08-20T14:30:00Z
updated: 2026-08-23T20:00:00Z
principal_stated_goal: "usando los datos del artículo, crea un ISA o completa/reelabora el existente para indroducir las novedades que marca el artículo, para cerrar la brecha entre ANKI/Buenas prácticas y Recall"
principal_stated_goal_source: prompt
principal_stated_goal_signal: 4
principal_stated_goal_locked: 2026-08-20T14:30:00Z
queued_runs: "run 4 legibility/palette/e-ink — ISC-76…ISC-85, Anti-13, Anti-14 · run 5 card media — ISC-86…ISC-101, Anti-15…Anti-18. Both specified, neither started; execution gated on run 3 reaching 87/87."
prior_run: "20260809-193403_recall-search-reader-api — ISC-1…ISC-25, Anti-1…Anti-3 (complete) · 20260817-193000_recall-llm-model-runtime-config — ISC-26…ISC-35, Anti-4…Anti-6 (complete)"
context_sufficient: true
interview_invoked: false
---

# Recall — ISA

> Project ISA. Long-lived system of record for the application: the claims Recall
> already satisfies (with evidence) plus the claims that do not hold yet.
> ISC IDs are stable and never renumbered. ISC-1…ISC-25 belong to the search /
> reader / API run, ISC-26…ISC-35 to the runtime LLM-model run; both record
> current state. ISC-36…ISC-75 is the memory-fidelity work, in progress.
> ISC-76…ISC-85 and ISC-86…ISC-101 are specified but not started — see
> `queued_runs` in the frontmatter for the gate.
>
> **This file is public** (`github.com/deleyva/recall`). No operator usage statistics,
> no host, user, port or absolute home paths — thresholds here are product targets,
> never a person's measured behaviour.

## Problem

Recall schedules well and measures nothing worth measuring.

The scheduler is FSRS, the storage is sound, the review log is complete — and yet the number the app reports back is not the number the user needs. Recall asks the learner to look at a question, turn the card over, and self-report whether they knew it. That measures **recognition**: whether a revealed answer feels familiar. Learners overwhelmingly want the opposite capability — **production**: saying the thing when nothing is in front of them. An app whose only instrument is self-graded recognition will report high retention to a user who cannot produce the material, and neither the user nor the algorithm has any way to notice.

Every other defect compounds that one:

- **The generator is instructed to produce non-atomic cards.** `DefaultFlashcardPrompt` in `internal/services/llm.go` spends five consecutive rules on HTML list formatting (`<ul><li>`, `<ol><li>`, "never use raw numbered text") and zero on atomicity. A card whose answer is a five-item list cannot be *failed*, only failed-partially — and the four rating buttons have no way to express that. The learner presses Hard or Good, the whole cluster gets a long interval, and the items that were not recalled are never counted as missed. This is a direct inversion of SuperMemo's rules 4, 9 and 10 (minimum information; avoid sets; avoid enumerations).
- **Cards generated from one article are siblings and are served consecutively.** The cron creates a whole batch per article in a single call, all with `due = now`; `GetNextDue` orders by `due ASC`, so the batch arrives as a block. Siblings share retrieval cues, so the first card primes the rest — the classic conditions for cue overload and retrieval-induced forgetting. Anki buries siblings by default precisely to prevent this; Recall has no such mechanism, and no schema slot for one.
- **Failure has no short loop.** `internal/scheduler/scheduler.go` sets `params.EnableShortTerm = false`, which disables FSRS's short-term scheduler entirely. `go-fsrs` v3.3.1's own `TestLongTermScheduler` shows the consequence: a failed review card returns in days, never in minutes, never in the same session. The first effortful re-retrieval — the moment recognition becomes production — never happens inside a session.
- **Intervals are unfuzzed.** `EnableFuzz` stays false, so cards created together and rated alike march together indefinitely. The first day's clustering becomes permanent.
- **There is nothing to slice the collection by.** The `cards` table carries `deck_id`, `front`, `back` and the FSRS columns. No `tags`, no `suspended`, no `buried_until`, no flags. The only study route is `/decks/:id/study`, so a collection that has accumulated in one deck cannot be cut by topic, source, or difficulty, and there is no way to build an ad-hoc session over a subset.
- **Bad cards are never detected.** `cards.lapses` is stored and never read. Anki flags a card as a leech at eight lapses and suspends it, on the reasoning that a card failed eight times is not hard, it is malformed. Recall re-serves it forever with no signal.
- **The one daily limit is on the wrong side.** `daily_card_limit` bounds how many cards the cron *generates*; nothing bounds how many are *studied*. New and review load are neither separated nor capped.

## Vision

You open a session and Recall makes you say it. The title, the name, the definition — typed or spoken before anything is revealed, with the app knowing whether you actually produced it rather than trusting your report. The number on the dashboard drops, and that drop is the good news: for the first time it means something. Cards arrive one idea at a time, never two siblings in a row, and when one is missed it comes back before you leave the session instead of two days later. When a card keeps losing, the app says so and offers to help you rewrite it. And when you need to drill one topic — one author, one source, everything you keep getting wrong — you can build that session in five seconds without wrecking the schedule you have earned everywhere else.

## Out of Scope

**FSRS parameter optimization on pre-change review history.** Optimizing a memory model against inflated self-grades produces longer intervals and amplifies the exact failure this work exists to fix. The optimizer is a later run, gated on a sustained period of honest grading — not part of this one.

Also excluded: changing the FSRS library version or upgrading to FSRS-5/6; a configurable desired-retention setting (same gating reason as the optimizer); image occlusion; mobile native apps; a spaced-repetition algorithm of our own; multi-user sharing of decks or tags; any frontend build step; semantic search or embeddings; automated rewriting or deletion of existing cards without per-card human confirmation.

**Out of scope for the memory-fidelity run, in scope as queued work.** Card media — images and audio — was excluded here on 2026-08-20 and is now specified as run 5 (ISC-86…ISC-101). It stays out of *this* run: it needs its own migration against the same `cards` table that sibling burying, tags and leech still have to touch, and interleaving two schema fronts is how migration numbering collides. Image occlusion remains excluded outright (Anti-17). Text-to-speech remains excluded: run 5 stores clips the operator supplies and synthesizes nothing.

## Principles

- **An instrument that cannot register failure is not an instrument.** Every scoring path must have a state that means "did not produce it," and card formulation must keep that state reachable. A card that can only be partially missed silently destroys the measurement.
- **Self-report is the weakest possible evidence, so the system asks for it last.** Where the app can observe the outcome — a typed answer compared against the expected one — it observes first and lets the human confirm, rather than asking the human to adjudicate their own recall from memory of a memory.
- **Scheduling state is sacred; presentation is not.** Hiding, ordering, filtering and burying may change *when* a card is shown. They may never silently mutate `due`, `stability`, `difficulty`, `state` or `lapses`. Any feature that blurs that line is a scheduling bug wearing a UI costume.
- **Granularity of the card is granularity of the algorithm.** A card carrying four facts is scheduled at the pace of its hardest fact. Atomicity is not a style preference; it is what lets the scheduler work per-item.
- **The learner's data is theirs, and destructive help is not help.** Anything that rewrites, splits or deletes existing cards proposes and waits. Bulk automation may never act on content the user has not seen.
- **Row-level isolation is absolute.** Every new read path filters by `user_id`; a new query is a new place to leak.

## Constraints

- Go 1.25, Echo v4, SQLite via `modernc.org/sqlite`, pure Go. `CGO_ENABLED=0 GOOS=linux GOARCH=amd64` cross-compilation must keep producing the deploy binary — no cgo-dependent extension.
- Schema changes go through goose migrations in `migrations/`, applied automatically at startup against a live, populated database. Every migration is rehearsed on a downloaded copy before it reaches a running instance.
- Frontend stays HTMX + Tailwind CDN + the per-page template registry. No bundler, no SPA, no npm.
- Auth stays session cookie for web and `Bearer rcl_...` for `/api/*`, enforced by `RequireAuth`.
- FSRS stays `go-fsrs` v3 for this run. The scheduler changes in scope are configuration flags on the existing library, not a version bump.
- Existing cards keep their scheduling state across every change in this ISA. No mass reschedule, no state reset.
- The scheduler is the only place allowed to write FSRS columns.

## Goal

"usando los datos del artículo, crea un ISA o completa/reelabora el existente para indroducir las novedades que marca el artículo, para cerrar la brecha entre ANKI/Buenas prácticas y Recall". Concretely: Recall gains the mechanisms that make a spaced-repetition system measure production rather than recognition — typed-answer production cards with a system-observed verdict, sibling burying, same-day relearning, interval fuzz, tags with filtered non-rescheduling study sessions, leech detection, separated daily study limits, and an atomic card-formulation prompt with a confirm-first splitter for legacy cards — each one verified by a probe, and the whole judged by a repeatable metrics command whose numbers move into the target bands.

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
- [x] ISC-29: `GET /profile` renders an input named `llm_model` carrying the stored value; `POST /profile` with a new value persists it; a subsequent `GET /profile` shows the new value. *(closed 2026-08-19 in a real browser; the earlier deferral blamed context UUID rot and was wrong — see Verification)*
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

### Memory fidelity — the instrument

- [x] ISC-36: `recall metrics` prints, for a given user, eight measures from the review log and card table: true retention over spaced reviews (`elapsed_days > 0`), the four-way rating distribution, the count of cards at or above the leech threshold, the share of reviews falling on the same day as a sibling of the same article, the share of card backs containing `<li>`, the share of card fronts containing a coordinating conjunction, the card count per deck, the review count per local hour, and — folded in during the build, because ISC-72 has no other probe — how often each rating is followed by a failure on the next review of that card. Output is stable, machine-readable, and re-runnable against any database copy.
- [x] ISC-37: `recall metrics` run twice against the same unchanged database file produces byte-identical output.

### Production, not recognition

- [x] ISC-38: Migration adds `cards.kind` with values `recognition` and `production`, defaulting to `recognition`, and applies cleanly on a populated copy without altering any existing FSRS column.
- [x] ISC-39: A `production` card's study view renders a text input and does not include the card's `back` anywhere in the served HTML until the learner submits.
- [x] ISC-40: The submitted answer is compared to the expected answer case-insensitively, accent-insensitively, and ignoring surrounding punctuation and whitespace; the answer view shows the typed answer against the expected one with the differences marked.
- [x] ISC-41: When the comparison finds no match, the answer view presents `Again` as the pre-selected rating; when it matches, `Good` is pre-selected. The learner can always override.
- [x] ISC-42: A `recognition` card's study flow is byte-for-byte the flow that exists today — no input box, no comparison, no change in served markup.
- [x] ISC-43: The reveal is server-side: a request that skips the submit step cannot obtain the `back` of a `production` card for the card currently in play.

### Sibling burying

- [x] ISC-44: Migration adds `cards.buried_until` (nullable timestamp) and applies cleanly on a populated copy.
- [x] ISC-45: Answering a card that has a non-null `article_id` sets `buried_until` to the next local day boundary for every other card of that article that is currently due and unanswered today — **except cards in learning or relearning**, which are never buried. *(tightened mid-run — see Decisions)*
- [x] ISC-46: `GetNextDue` excludes any card whose `buried_until` is in the future, and includes it again once that timestamp has passed. Every other count of due cards — deck list, deck overview, dashboard stats, the `due_only` card API — excludes them too, or the number promises work the queue will not serve. *(widened mid-run — see Decisions)*
- [x] ISC-47: Burying a card changes no FSRS column — a row snapshot of `due`, `stability`, `difficulty`, `elapsed_days`, `scheduled_days`, `reps`, `lapses`, `state`, `last_review` before and after a bury is identical.
- [x] ISC-48: Deck overview offers an unbury control that clears `buried_until` for that deck, and after using it the previously buried cards are served again in the same session.
- [ ] ISC-49: Measured by `recall metrics` over the 30 days following deployment, the share of reviews falling on the same day as a sibling of the same article is below 20%.

### Scheduler honesty

- [x] ISC-50: `EnableFuzz` is true, and a test proves a review interval of 2.5 days or more is dispersed rather than returned exactly — the same card state scheduled across a sweep of review seconds yields more than one distinct interval, every one of them inside the library's documented fuzz range, where the same sweep with fuzz off yields exactly one. *(restated mid-run — see Changelog)*
- [x] ISC-51: `EnableShortTerm` is no longer forced false, and a test proves a review card rated `Again` receives a next interval under 24 hours rather than the multi-day interval the long-term-only scheduler produces.
- [x] ISC-52: Failing a card during a study session causes that card to be served again within the same session — probed end to end against a running server, not asserted from the scheduler alone.
- [x] ISC-53: Enabling both flags rewrites no existing row: a before/after snapshot of every card's FSRS columns across a server restart with the new configuration is identical.
- [x] ISC-74: When nothing is due, the queue serves a learning or relearning card whose due date falls inside a bounded look-ahead window, and never pulls a review card forward. Without it a failed card's five-minute step outlives the session and the short loop restored by ISC-51 never closes.
- [x] ISC-75: The short loop terminates: two `Good` ratings take a new card from New through Learning to Review with an interval of at least a day. This is the anti-regression for migration 010, which exists because cards once accumulated in Learning and cycled.

### Tags and filtered study

- [x] ISC-54: Migration adds a tag store allowing many tags per card, storing each tag as a normalized `key` plus its display form, shaped `dominio/tema` at a fixed depth of two with the first segment drawn from a closed list, and applies cleanly on a populated copy. *(restated 2026-08-23 — the `::` free-depth hierarchy was refuted; see Changelog)*
- [x] ISC-55: Cards created by the flashcard generator and by the Readeck sync are tagged automatically from their source article, so tags accumulate with no manual step. The generator picks the domain from the closed list and may not invent a first segment — a tag store nobody types into cannot drift.
- [x] ISC-56: A backfill assigns at least one tag, derived from the source article, to every existing card that has an `article_id`; cards without one are reported rather than silently skipped.
- [x] ISC-57: A study session can be built from a tag filter, a minimum-lapses filter, or both, spanning every deck the user owns, and it never serves another user's card.
- [x] ISC-58: A filtered session offers a no-reschedule mode; studying a full session in that mode leaves `due`, `stability`, `difficulty`, `state`, `reps` and `lapses` unchanged for every card served, verified by a row snapshot before and after, **and writes no `review_logs` row either**. *(widened mid-run — see Decisions)*
- [x] ISC-59: A filtered session in normal mode does reschedule, and a review performed there is written to `review_logs` exactly as one performed in a deck session.

### Leech detection

- [x] ISC-60: A card reaching the leech threshold (8 lapses) is flagged, and the flag is visible on the card during study — on the question and on the answer, so the learner sees it before deciding to attempt it again.
- [x] ISC-61: A leech list is reachable from the dashboard and offers, per card, the three documented remedies: edit the card, delete it, or suspend it.
- [x] ISC-62: Migration adds `cards.suspended`; a suspended card is never served by any study path — deck, filtered, or otherwise — nor counted by any due count, and unsuspending restores it with its FSRS state untouched. *(widened mid-run — see Decisions)*

### Atomic formulation

- [x] ISC-63: `DefaultFlashcardPrompt` states the minimum-information principle: one idea per card, an answer of a single element, no enumerations (a source list of N items becomes N cards), no coordinating conjunction in the question, HTML reserved for emphasis rather than list structure.
- [ ] ISC-64: For any source passage containing a named work, author, or other proper name, the generator emits the pair in both directions — name → claim and claim → name — with the claim → name card marked `kind = production`. `[DEFERRED-VERIFY]` — the instruction is in the prompt and the `kind` the generator returns is carried through the parser into the column, both proven; the claim is about what a live generation actually emits, and there is no key on this machine to run one. Closes on a generation against the deployed instance, as ISC-32 and ISC-33 did.
- [ ] ISC-65: Measured by `recall metrics` over cards generated in the 30 days after the prompt change, fewer than 10% of backs contain `<li>` and fewer than 10% of fronts contain a coordinating conjunction.
- [x] ISC-66: A splitter tool lists every existing card whose back contains a list or whose front contains a coordinating conjunction, proposes a split into atomic cards, and writes nothing until the operator confirms that specific card.
- [x] ISC-67: Running the splitter with no confirmation leaves the card table byte-identical, proven by a checksum of the table before and after a dry run.

### Study load

- [x] ISC-68: New-card and review-card daily study limits are separate per-user settings, enforced by the study queue rather than by the generator — and every count of due cards is capped by the same budget, not merely filtered by it. *(widened mid-run — see Changelog)*
- [x] ISC-69: The generation limit and the study limits are distinct, separately labelled settings in the profile UI and in the account API, and changing one does not change the other.

### Outcomes

- [x] ISC-70: Antecedent: every card whose answer is a proper name, title, or other arbitrary label is `kind = production`, so the session cannot be completed without producing those answers unaided. This is the precondition for the experiential goal — the name arriving unprompted — and no downstream outcome claim is credible without it.
- [ ] ISC-71: Sixty days after production cards ship, true retention measured by `recall metrics` lies between 75% and 90%. Above 95% means the instrument is still measuring recognition and the claim fails.
- [ ] ISC-72: Over the same window, the share of reviews rated `Hard` is below 10%, and `Hard` predicts a subsequent `Again` at least twice as often as `Good` does — evidence that the middle button carries information rather than noise.
- [ ] ISC-73: In a live unaided check over ten randomly drawn `production` cards whose answer is a title or proper name, at least eight are produced correctly before reveal.

### Anti-criteria (memory fidelity)

- [ ] Anti-7: FSRS parameters are NOT optimized against review history recorded before production cards shipped. No optimizer run, no weight file, no `RequestRetention` change in this work.
- [ ] Anti-8: No automated process edits, splits, merges or deletes an existing card without explicit per-card operator confirmation.
- [ ] Anti-9: No migration in this ISA alters an existing card's FSRS columns — proven by a full-column snapshot diff across each migration on a populated copy.
- [ ] Anti-10: No frontend build step, bundler, or npm dependency is introduced.
- [ ] Anti-11: `go test ./...` stays green, including the search, API-spec and auth suites from the prior run.
- [ ] Anti-12: No study path serves a card belonging to another user, including the new filtered and leech paths.

### Legibility, palette and e-ink *(queued — run 4; not started. Goal: "me gustaría que fuera una aplicación minimalista en la que los colores fueran algo pastel y se viese lo mejor posible en un dispositivo de tinta electrónica con mucho contraste")*

Target device is an Android e-ink tablet with a real browser, so the app is
rendered on the panel rather than exported to it. The two halves of the request
pull against each other — pastels are high-value, low-saturation colours, and a
16-level grayscale panel collapses them into two or three indistinguishable
greys — so the resolving rule is stated as a principle rather than a palette:
colour is never the only channel that carries meaning. Once every state also
differs in value, weight, border or label, the pastels are free, because they
are decoration.

- [ ] ISC-76: Every colour on the study surface — base layout, `study.html`, and the four study partials — comes from a named token declared in one place; no template in that set names a Tailwind palette colour directly (`text-green-700`, `bg-gray-800`, `dark:text-white`). Falsifier: a grep for palette-colour classes over that file set returns a hit.
- [ ] ISC-77: The same holds for every remaining template. The grep is repo-wide and returns nothing; the three themes are defined only in the token block.
- [ ] ISC-78: Every foreground/background token pair used for text meets WCAG AA — 4.5:1 for body, 3:1 for large — in all three themes, proven by a computed contrast report over the token table rather than by eye. A pastel is admissible as a surface or an accent, never as text on another pastel.
- [ ] ISC-79: The card back renders in the same token as body text in every theme. There is no colour that means "this is the answer". This is the defect that opened the run: `study_answer_partial.html` renders the back as `text-green-700 dark:text-white`, and green 700 on the dark card surface is roughly 1.9:1.
- [ ] ISC-80: Lists inside card fronts and backs render with visible markers and indentation in all three themes. Today the list CSS in `base.html` is keyed on the colour classes that carry the text (`.text-green-700 ul`, `.dark .text-green-400 ul`), so a card back that is white in dark mode matches neither selector and loses its bullets — exactly the material the generator produces most of.
- [ ] ISC-81: Rendered at 16 levels of grey, the study page, the answer page and the dashboard keep every state distinguishable: Produced vs Not produced, the four rating buttons, due vs buried, flagged vs not. Each is carried by a label or a shape as well as a hue. Falsifier: two states that are one grey apart and share their text.
- [ ] ISC-82: E-ink is a third selectable theme, not dark mode with adjustments. In e-ink mode the served CSS contains no `transition`, no `animation`, no `box-shadow`, and no text rendered below full opacity; HTMX swaps replace content without a fade.
- [ ] ISC-83: In e-ink mode every interactive control has a hit area of at least 48×48 CSS px and a solid border of at least 1px. A ghost button — colour fill with no outline — is invisible on the panel.
- [ ] ISC-84: Verified on the device: a full card cycle — front, typed answer, comparison, the four ratings — photographed on the e-ink tablet, every element legible, nothing depending on a partial refresh that leaves ghosting where the answer will appear.
- [ ] ISC-85: The chosen theme applies before first paint in all three cases and survives a reload and a stack restart. The current inline script covers dark only.

### Card media — images and audio *(queued — run 5; not started. Goal: "me gustaría añadir las capacidades que tiene Anki de añadir audios o de añadir imágenes... si vas a estudiar Historia del Arte o la cara de la gente de la que hablas")*

Media is the input side of a mechanism that already exists rather than a new
pillar. A painting, a face or a musical phrase is arbitrary-label material, and
ISC-70 already requires arbitrary labels to be `production` cards — so an image
on the front with a typed name behind the reveal gate is the production card of
ISC-38…ISC-43 fed a different prompt. The generator cannot produce a picture or
a clip, so every media card is authored by hand; both authoring routes are in
scope, because a bulk folder covers preparing a teaching unit and a per-card
control covers the single fix.

- [ ] ISC-86: Migration adds a `media` table — content-addressed by SHA-256, scoped by `user_id`, carrying mime, byte length and the bytes — and applies cleanly on a populated copy, altering no FSRS column and no existing card row.
- [ ] ISC-87: Media is referenced inline from the existing card fields as `<img src="/media/:id">` and `<audio src="/media/:id">`. No new column on `cards`, so media composes unchanged with production cards, search indexing, the card editor, sibling burying and the splitter.
- [ ] ISC-88: Field HTML is sanitized on render against a tag and attribute allowlist rather than passed through `safeHTML`. For every input in a hostile-input table — `<script>`, `<svg onload>`, `<img onerror>`, a `javascript:` source, a `data:` source, an external `http://` source — the rendered output is inert.
- [ ] ISC-89: Upload accepts JPEG, PNG, WebP, MP3, M4A and OGG and nothing else, decided by sniffing the stored bytes rather than by the declared content type or the filename. SVG is rejected, because an SVG is a script container.
- [ ] ISC-90: Images are decoded and re-encoded server-side to a bounded maximum dimension in pure Go, and the stored bytes are the re-encoded ones. The uploaded original is never persisted.
- [ ] ISC-91: Per-file size caps are enforced before the bytes reach the database, and a request over the cap is refused without the whole body being buffered.
- [ ] ISC-92: `GET /media/:id` serves the stored bytes with the stored mime, `X-Content-Type-Options: nosniff`, and a long cache lifetime, which is safe because the id is the content hash. Requesting another user's media id returns 404 and discloses nothing — including whether the id exists.
- [ ] ISC-93: Identical bytes uploaded twice produce one row. A painting used on ten cards is stored once.
- [ ] ISC-94: The card editor offers attach-image and attach-audio controls on both front and back; the control uploads and inserts the reference where the cursor is.
- [ ] ISC-95: A bulk import command reads a directory and proposes one card per file — front is the media reference, back is the filename stem with separators normalized — reporting the whole set and writing nothing until the operator confirms.
- [ ] ISC-96: A card whose front is an image defaults to `kind = production`, and the typed-answer comparison of ISC-40 works against it unchanged.
- [ ] ISC-97: Audio plays from an explicit control, never on load. Where the audio *is* the answer it sits behind the same server-side reveal gate as ISC-43 — a request that skips the submit step cannot obtain the clip.
- [ ] ISC-98: A garbage-collection command lists media no card references any more and deletes nothing until confirmed; the dry run leaves the media table byte-identical, proven by a checksum before and after.
- [ ] ISC-99: Restoring from the backup artifact alone reproduces every card's media. Whatever the backup covers today must still be everything after this run — the blind spot being closed is that media stored outside the database would make the existing backup silently partial.
- [ ] ISC-100: An image card is answerable on the e-ink panel — ten real art-history images studied on the device, the result recorded whatever it is. A negative result is the finding, not a failure to report: dithered grayscale may not carry a face.
- [ ] ISC-101: `recall metrics` reports media cards and their retention separately from text cards, so the claim that pictures help is measured rather than assumed.

### Anti-criteria (queued runs)

- [ ] Anti-13: No frontend build step, bundler or npm dependency is introduced by the palette work. The three themes are Tailwind Play CDN configuration plus a token block.
- [ ] Anti-14: The palette refactor changes presentation only — the diff across every template touches `class` and `style` attributes and nothing else. No `hx-` attribute, target, route or form field changes.
- [ ] Anti-15: No cgo. Image decoding, resizing and encoding stay pure Go, and `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` still produces the deploy binary.
- [ ] Anti-16: No media path accepts or serves another user's row.
- [ ] Anti-17: Image occlusion is NOT built. Static images and audio clips only.
- [ ] Anti-18: No media byte is written outside the database. There is exactly one artifact to back up, as there is today.

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
| ISC-36 | cli | run `recall metrics` against a populated copy | eight measures present, parseable | bash + go run | principal_stated_goal |
| ISC-37 | cli | run twice, diff output | no difference | bash `cmp` | Principles |
| ISC-38 | schema | apply migration on populated copy, read schema + column snapshot | column present, FSRS columns unchanged | sqlite3 + goose | Constraints |
| ISC-39 | http | fetch study page for a production card, grep body for the back text | 0 occurrences | curl -i | Vision |
| ISC-40 | unit | table test over accent/case/punctuation variants | every variant matches | go test | Principles |
| ISC-41 | web/UI | submit a wrong answer, screenshot the answer view | Again pre-selected | Interceptor | Principles |
| ISC-42 | http | diff served markup for a recognition card before/after the change | identical | curl + diff | Anti-2 |
| ISC-43 | http | request the answer route without submitting | back not disclosed | curl -i | Principles |
| ISC-44 | schema | apply migration on populated copy | column present | goose + sqlite3 | Constraints |
| ISC-45 | unit | answer one card of a multi-card article, read siblings | siblings buried to next day boundary | go test | Problem |
| ISC-46 | unit | queue query with a future and a past `buried_until` | excluded then included | go test | Problem |
| ISC-47 | unit | snapshot all FSRS columns before/after a bury | identical | go test | Principles |
| ISC-48 | web/UI | bury, then unbury from deck overview, resume study | cards served again | Interceptor | Vision |
| ISC-49 | measurement | `recall metrics` after 30 days of use | same-day sibling share < 20% | SELECT | Goal |
| ISC-50 | unit | sweep review seconds over one card state, with fuzz on and off | >1 interval on, exactly 1 off, all inside FUZZ_RANGES | go test | Problem |
| ISC-74 | unit | queue with a learning card inside and outside the window | served inside, withheld outside, reviews never pulled forward | go test | Vision |
| ISC-75 | unit | two Goods from New | state reaches Review, interval >= 1 day | go test | Constraints |
| ISC-51 | unit | rate a review card `Again`, read next interval | < 24h | go test | Problem |
| ISC-52 | web/UI | fail a card in a live session, continue studying | same card reappears | Interceptor | Vision |
| ISC-53 | schema | full card-table column snapshot across restart | identical | sqlite3 + cmp | Constraints |
| ISC-54 | schema | apply migration on populated copy; insert two tags differing only in accents and case | tag store present, many-to-many; the two collapse to one key | goose + sqlite3 | Constraints |
| ISC-55 | integration | generate cards from an article, read tags | tags present without manual step | go test | Vision |
| ISC-56 | migration | run backfill on populated copy, count untagged | every card with article_id tagged; rest reported | SELECT | Problem |
| ISC-57 | web/UI | build a session by tag and by min-lapses | correct set, cross-deck, own user only | Interceptor + SELECT | Vision |
| ISC-58 | unit | snapshot rows, study a full no-reschedule session, snapshot again | identical | go test | Principles |
| ISC-59 | unit | study a normal filtered session | rows updated, review_logs written | go test | Principles |
| ISC-60 | web/UI | study a card at the leech threshold | flag visible | Interceptor | Problem |
| ISC-61 | web/UI | open leech list from dashboard | list renders, three remedies present | Interceptor | Vision |
| ISC-62 | unit | suspend a card, drive every study path | never served; unsuspend restores state | go test | Problem |
| ISC-63 | file | read the prompt constant | states atomicity rules, no list mandate | Read + Grep | Problem |
| ISC-64 | integration | generate from a passage naming a work | both directions emitted, reverse marked production | go test | Goal |
| ISC-65 | measurement | `recall metrics` over the post-change generation window | both shares < 10% | SELECT | Goal |
| ISC-66 | cli | run splitter against a populated copy | candidates listed, proposals shown | bash | Vision |
| ISC-67 | cli | checksum card table before/after a dry run | identical | sqlite3 + cmp | Anti-8 |
| ISC-68 | unit | exceed each limit independently | queue stops at the right count | go test | Problem |
| ISC-69 | http | change each setting via the account API | the other is unchanged | curl -i | Problem |
| ISC-70 | schema | classify answer types, count production cards among them | every proper-name answer is production | SELECT | Vision |
| ISC-71 | measurement | `recall metrics` at day 60 | 75% ≤ retention ≤ 90% | SELECT | Goal |
| ISC-72 | measurement | rating distribution + next-review-after-rating query | Hard < 10%; Hard→Again ≥ 2× Good→Again | SELECT | Goal |
| ISC-73 | experiential | ten random production title cards, unaided, answers recorded before reveal | ≥ 8 produced | manual, logged | Vision |
| Anti-7 | file+config | grep the scheduler and repo for optimizer/weight/retention changes | none present | Grep | Out of Scope |
| Anti-8 | cli | drive every bulk tool without confirming | zero writes | sqlite3 checksum | Principles |
| Anti-9 | schema | column snapshot diff per migration | identical FSRS columns | sqlite3 + cmp | Constraints |
| Anti-10 | build | inspect repo for bundler/npm artefacts | none | Glob + Read | Constraints |
| Anti-11 | test | `go test ./...` | green | go test | Constraints |
| Anti-12 | http | drive every study path with a foreign card id | no disclosure | curl -i | Principles |
| ISC-76 | file | grep the study surface for Tailwind palette-colour classes | zero hits | Grep | principal_stated_goal |
| ISC-77 | file | same grep, repo-wide over templates | zero hits | Grep | principal_stated_goal |
| ISC-78 | computed | contrast ratio over every text token pair, three themes | ≥ 4.5:1 body, ≥ 3:1 large | script + token table | Principles |
| ISC-79 | file+browser | read the back's token, then screenshot the answer view in each theme | back token == body token | Read + Interceptor | principal_stated_goal |
| ISC-80 | browser | render a card back containing `<ul>` and `<ol>` in each theme | markers and indent visible in all three | Interceptor | Problem |
| ISC-81 | browser | screenshot study, answer and dashboard, reduce to 16 greys | every state still distinguishable | Interceptor + convert | principal_stated_goal |
| ISC-82 | http | fetch the page in e-ink mode, scan the served CSS | no transition/animation/shadow, no faded text | curl + grep | principal_stated_goal |
| ISC-83 | browser | measure every control's box in e-ink mode | ≥ 48×48 px, border ≥ 1px | Interceptor | principal_stated_goal |
| ISC-84 | experiential | full card cycle on the e-ink tablet, photographed | every element legible | device, photo logged | principal_stated_goal |
| ISC-85 | browser | load each theme cold, reload, restart the stack | no flash, choice preserved | Interceptor | principal_stated_goal |
| ISC-86 | schema | `goose up` on a copy, then column snapshot diff | table exists, FSRS columns identical | sqlite3 + cmp | Constraints |
| ISC-87 | schema | inspect `cards` after the migration | no new column | sqlite3 | Principles |
| ISC-88 | unit | hostile-input table through the sanitizer | every case inert | go test | Principles |
| ISC-89 | unit+http | upload each allowed and disallowed type, plus a renamed SVG | allowlist decided by bytes | go test + curl | Principles |
| ISC-90 | unit | upload an oversized image, read back the stored bytes | re-encoded, bounded, original absent | go test | Constraints |
| ISC-91 | http | POST a body over the cap | refused, not buffered whole | curl -i | Constraints |
| ISC-92 | http | fetch own media, then another user's id | 200 with nosniff; 404 | curl -i | Principles |
| ISC-93 | unit | upload identical bytes twice | one row | go test | principal_stated_goal |
| ISC-94 | browser | attach an image and a clip from the editor, front and back | reference inserted, card renders | Interceptor | principal_stated_goal |
| ISC-95 | cli | run the importer against a folder without confirming | proposal printed, zero writes | sqlite3 checksum | Anti-8 |
| ISC-96 | http | create an image-front card, study it | kind is production, comparison works | curl -i + Interceptor | ISC-70 |
| ISC-97 | http | request the answer of an audio-answer card without submitting | clip not served | curl -i | ISC-43 |
| ISC-98 | cli | dry-run the media GC | table checksum identical | sqlite3 + cmp | Anti-8 |
| ISC-99 | restore | restore the backup artifact into a clean instance | every card's media present | docker + browser | Constraints |
| ISC-100 | experiential | ten art-history image cards on the e-ink device | result recorded either way | device, logged | Vision |
| ISC-101 | cli | `recall metrics` on a copy with media cards | media split reported | recall metrics | Principles |
| Anti-13 | build | inspect the repo for bundler/npm artefacts | none | Glob + Read | Constraints |
| Anti-14 | diff | `git diff` over the palette commit, filtered by attribute | only class/style hunks | git diff | Principles |
| Anti-15 | build | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build` | binary produced | go build | Constraints |
| Anti-16 | http | every media path with a foreign id | no disclosure | curl -i | Principles |
| Anti-17 | file | grep the repo for occlusion code | none present | Grep | Out of Scope |
| Anti-18 | file | inspect the container volume after uploads | no media file on disk | docker exec + ls | Constraints |

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
| metrics-command | `recall metrics` reading the review log and card table, stable machine-readable output, runnable against any copy — the instrument every outcome claim is probed with | ISC-36, ISC-37 | — | no |
| production-cards | `cards.kind` migration, typed-answer study view, server-side reveal, normalized comparison, diff display, rating pre-selection | ISC-38…ISC-43, ISC-70 | metrics-command | no |
| bury-siblings | `buried_until` migration, bury-on-answer for same-article siblings, queue filter, unbury control | ISC-44…ISC-49 | — | yes (with production-cards) |
| scheduler-honesty | `EnableFuzz` on, forced `EnableShortTerm=false` removed, same-session relearning proven end to end, no-rewrite proof | ISC-50…ISC-53 | — | yes (with bury-siblings) |
| tags-and-filtered-study | tag store migration, auto-tagging on generation and sync, backfill, cross-deck filtered sessions by tag and lapses, no-reschedule mode | ISC-54…ISC-59 | — | no |
| leech-detection | leech flag at threshold, `suspended` column, leech list with edit/delete/suspend remedies | ISC-60…ISC-62 | metrics-command | yes (with tags-and-filtered-study) |
| atomic-generation | rewritten `DefaultFlashcardPrompt`, bidirectional pair emission for named things, confirm-first splitter for legacy cards with dry-run proof | ISC-63…ISC-67 | production-cards | no |
| study-limits | separate new/review daily study limits enforced in the queue, relabelled distinctly from the generation limit | ISC-68, ISC-69 | — | yes (with atomic-generation) |
| verify | migration rehearsal on a downloaded copy, browser pass through Interceptor, build/vet/test sweep, isolation sweep across the new paths | ISC-39, ISC-41, ISC-48, ISC-52, ISC-57, ISC-60, ISC-61, Anti-9…Anti-12 | all | no |
| outcome-watch | scheduled `recall metrics` runs at day 30 and day 60 against a copy, plus the unaided ten-card check | ISC-49, ISC-65, ISC-71…ISC-73 | all | no |
| design-tokens | Three-theme token block in `base.html` (light, dark, eink) wired through the Play CDN config, then the study surface converted off raw palette classes | ISC-76, ISC-78, ISC-79, ISC-80, Anti-13, Anti-14 | run 3 complete | no |
| theme-sweep | The remaining templates converted to tokens; theme selector, persistence, and pre-paint application for all three | ISC-77, ISC-85 | design-tokens | no |
| eink-mode | E-ink theme: motion and shadow stripped, hit areas and borders enlarged, HTMX swaps made flat | ISC-82, ISC-83 | design-tokens | no |
| legibility-verify | Grayscale reduction pass, contrast report over the token table, on-device photographed card cycle; also closes the long-deferred ISC-13 and ISC-19 | ISC-81, ISC-84, ISC-13, ISC-19 | theme-sweep, eink-mode | no |
| media-store | `media` table migration, content-addressed dedup, pure-Go re-encode, size caps, allowlist by byte sniffing, user-scoped serving with nosniff | ISC-86, ISC-89…ISC-93, Anti-15, Anti-16, Anti-18 | run 3 complete | yes (with design-tokens) |
| field-sanitizer | Allowlist sanitizer replacing bare `safeHTML` on card fields, admitting `img` and `audio` and nothing dangerous | ISC-87, ISC-88 | media-store | no |
| media-authoring | Attach controls in the card editor, bulk folder importer with confirm-first proposal, media GC dry-run | ISC-94, ISC-95, ISC-98 | field-sanitizer | no |
| media-study | Image fronts default to production, typed comparison unchanged, audio behind an explicit control and behind the reveal gate when it is the answer | ISC-96, ISC-97 | field-sanitizer | no |
| media-verify | Backup restore proof, on-device image legibility check, metrics split for media cards | ISC-99…ISC-101, Anti-17 | media-authoring, media-study | no |

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
- 2026-08-20 14:30: **refined:** this file's Goal moved from the search/reader/API run to the memory-fidelity work. The prior run's ISC-1…ISC-25 and Anti-1…Anti-3 are retained verbatim with their IDs and checkmarks as the record of current state, per the project-ISA rule that the file outlives any single run. ISC-13 and ISC-19 stay open.
- 2026-08-20 14:30: The metrics command is built **first**, before any behaviour change. Four of the new claims are outcome claims stated as measured bands; without a repeatable instrument they would close on assertion. Building the instrument first also captures a pre-change baseline that the post-change numbers are compared against.
- 2026-08-20 14:30: FSRS optimization and configurable desired retention pushed to Out of Scope rather than included. Optimizing against self-graded recognition data would lengthen intervals and deepen the defect; the optimizer only becomes safe after a sustained period of grading that registers failure. Recorded as Anti-7 so the exclusion is probeable rather than merely stated.
- 2026-08-20 14:30: Typed-answer comparison **pre-selects** a rating rather than assigning one. Auto-grading would replace one bad instrument with another — a near-miss, a synonym, or a different valid phrasing are the learner's call. The system supplies the observation; the human keeps the verdict.
- 2026-08-20 14:30: Reveal is enforced server-side (ISC-43) rather than by hiding the answer in the DOM. An answer present in the served HTML is an answer available to anyone who looks, which is precisely the failure mode production cards exist to remove.
- 2026-08-20 14:30: Sibling burying keyed on `article_id`, which already exists on `cards`, rather than introducing a note/sibling model. Anki's siblings are cards of one note; Recall's de-facto siblings are cards generated from one article. Reusing the existing column gets the mechanism without a data model migration.
- 2026-08-20 14:30: Legacy card cleanup kept in scope but strictly confirm-first, with a dry-run checksum proof (ISC-67, Anti-8). Cards accumulated over months are the operator's own writing; a bulk rewrite is not a refactor.
- 2026-08-20 14:30: This file carries no operator usage statistics. `ISA.md` is tracked and the remote is public (`"visibility": "public"` from the GitHub API, checked this run), so measured personal behaviour — retention figures, study hours, per-deck counts — lives only in the private analysis, and this file states thresholds as product targets. The baseline the outcome claims compare against is captured by `recall metrics` at build time, not written here.
- 2026-08-21 10:30: `EnableShortTerm = false` was not an oversight, and reverting it needed more than flipping the flag. `migrations/010_fix_stuck_learning.sql` exists because cards had accumulated in the Learning state and cycled, and switching to the long-term scheduler was the fix. That cure removed same-day relearning altogether. The pile-up is handled where it belongs instead — the queue serves cards mid-loop first and the loop provably terminates (ISC-74, ISC-75) — rather than by keeping a scheduler that cannot express failure. Recorded here because the next person to read that migration will otherwise think this change undoes a considered decision without knowing it was reconsidered.
- 2026-08-21 10:30: Restoring short-term scheduling changes new cards too, not just failures: a new card rated Good now waits ten minutes and graduates on the second Good, instead of jumping straight to a multi-day interval. That is Anki's default and it is the in-session repetition the whole feature exists to restore, but it is a visible change to a daily habit and is called out rather than discovered.
- 2026-08-21 10:30: Learn-ahead is a last resort, not a priority. The first cut ranked cards mid-loop above everything and a unit test caught it serving a card due in five minutes ahead of one due now. The ordering is three terms: actually-due before looked-ahead, then mid-loop before new before review, then due date. Studying ahead of the schedule is what spacing exists to prevent, so it may only fill an otherwise empty queue.
- 2026-08-21 10:30: Learn-ahead covers learning and relearning states only. Reaching forward for a review card would be cramming with extra steps.
- 2026-08-20 19:20: The card-kind migration is numbered **015, not 014**. The live database already had version 14 recorded as applied (2026-08-17) from a migration that is not in this tree. Reusing 014 would have been silently skipped by goose and the app would have started against a schema with no `kind` column. Found by rehearsing on a copy of the live database rather than on a fresh one — a fresh database would have applied 014 happily and told us nothing. Numbering a migration now means reading the deployed `goose_db_version`, not the files on disk.
- 2026-08-20 19:20: The reveal gate is a session value, not a client-side flag. A production card's answer is withheld until the server has seen an attempt for that specific card, and grading it clears the mark so the next card has to be earned. A hidden field or a DOM class would be a suggestion; this is a gate.
- 2026-08-20 19:20: `blockedAsProduction` returns `(handled bool, err error)` rather than an error alone. Echo's `c.String` returns nil on success, so an error-only guard wrote the 403 and then rendered the answer into the same response. The web test caught it; the signature now makes the mistake unavailable.
- 2026-08-20 19:20: The kind switch lives in the existing edit form rather than on a new endpoint. The learner is already in that view when they decide a card should be produced rather than recognised, and an absent `kind` value leaves the card alone, so saving a wording fix never silently changes how a card is asked.
- 2026-08-20 19:20: The typed answer is compared on a fold that drops case, diacritics and punctuation, and the diff shows the *stored* spelling for words the learner got right. Scoring a correct recall as a failure over a missing tilde would teach the learner to distrust the instrument, which is the one thing this feature cannot afford.
- 2026-08-20 17:40: `recall metrics` computes the per-rating followup matrix that ISC-72 is stated in terms of. ISC-72 was written with no probe that could produce its number; rather than leave a claim that could only close on a hand-written query, the measure was folded into the instrument and ISC-36's wording extended to name it.
- 2026-08-20 17:40: Day boundaries and hours use the requested zone rather than UTC. A session that runs past midnight local time belongs to the day the learner thinks it does, and the sibling-interference measure is about what happened in one sitting. This shifts the measure slightly against any UTC-based hand query — the tool's number is the definition from here on.
- 2026-08-20 17:40: The review pass is computed in Go from one ordered query rather than in SQL with window functions. The hour histogram needs zone conversion in Go anyway, the followup matrix needs a per-card walk, and one ordered pass gives both without depending on which SQLite build `modernc.org/sqlite` happens to ship.
- 2026-08-20 17:40: Conjunction matching covers `y`, `e` and `and` — coordination only. A disjunction ("X o Y") is usually one question offering alternatives rather than two questions in one card, so including it would inflate the measure with cards that are not malformed. Punctuation is flattened to spaces first so "X, y Z" counts.
- 2026-08-20 14:30: Dead end considered and rejected — adding a "partially correct" fifth rating button to fix the un-failable-list problem. It would let a malformed card survive by making its failure expressible instead of removing the malformation, and FSRS has no fifth grade to map it to. The fix is atomic cards (ISC-63), not a richer excuse.


- 2026-08-22 09:00: Two new goals recorded — a minimalist pastel palette that reads on an e-ink panel, and Anki-style image and audio on cards — and **queued rather than started**. Run 3 stands at 54 of its criteria closed, and everything still open in it (sibling burying, tags, leech and suspend, the splitter) needs migrations against a live populated database. Media needs its own migration and touches the same `cards` table and the same study templates. One person alternating two schema fronts is where migration numbering collides, which is the failure migration 015 was renumbered to avoid two days ago. Criteria are written now so the work is specified while the reasoning is fresh; execution waits for 87/87.
- 2026-08-22 09:00: The e-ink target is a real Android e-ink tablet with a browser, not an export format. That makes the criteria verifiable on the device (ISC-84, ISC-100) rather than by simulation, and it makes e-ink a third runtime theme rather than a print stylesheet.
- 2026-08-22 09:00: "Pastel" and "maximum contrast on e-ink" are opposing targets and are resolved by a principle rather than by a compromise palette. Pastels are high-value, low-saturation colours; a 16-level grayscale panel maps them onto two or three indistinguishable greys. So colour is never the only channel: every state carries a label, a border, a weight or a shape as well as a hue (ISC-81). Under that rule the pastels cost nothing, because they are decoration, and the panel loses no information.
- 2026-08-22 09:00: The palette is centralized into tokens (ISC-76, ISC-77) rather than fixed where it hurts. The reported defect — the card back unreadable in dark mode — exists because 156 `dark:` utilities are scattered across 19 templates with no single place that defines what a surface or a body colour is. Patching the one div would leave the same class of bug live everywhere else, and the contrast claim would stay uncheckable. Anti-14 keeps that refactor honest by requiring the diff to touch presentation attributes only.
- 2026-08-22 09:00: The answer text gets no colour of its own (ISC-79). A dedicated "answer green" is a colour carrying meaning, which the e-ink rule forbids, and it was never doing work that position and a rule above it were not already doing.
- 2026-08-22 09:00: Media is referenced inline from the existing card fields rather than given columns on `cards` (ISC-87). Anki does the same thing, and it is what makes media compose for free with production cards, search, the editor, burying and the splitter instead of requiring each of them to learn about a new attachment concept.
- 2026-08-22 09:00: Media bytes live in the database, not on a volume (Anti-18). The whole application is one SQLite file today, and that is what the backup covers. Files on disk would make the existing backup silently partial — the worst failure mode available, because nothing reports it until a restore. A single-user collection of a few hundred images and clips is tens of megabytes; SQLite carries that without complaint, and ISC-99 proves the restore.
- 2026-08-22 09:00: Uploads force a sanitizer onto the card fields (ISC-88). They are rendered today through `safeHTML`, which was tolerable while the only writers were the LLM and the operator. An upload path plus `<img>` and `<audio>` in field HTML makes an allowlist necessary, and SVG is refused outright (ISC-89) because an SVG is a script container wearing an image extension.
- 2026-08-22 09:00: Both authoring routes are built rather than one. A per-card attach control is the wrong shape for thirty paintings in a Baroque unit, and a folder importer is the wrong shape for fixing one card — and the importer is the cheaper of the two, since the filename is already the answer.
- 2026-08-22 09:00: Image cards default to `production` (ISC-96) rather than being a new card kind. A painting, a face or a musical phrase is arbitrary-label material, and ISC-70 already requires arbitrary labels to be produced. Media is the input side of a mechanism that shipped in run 3, not a pillar of its own.
- 2026-08-22 09:00: ISC-100 is written so a negative result closes it. Faces on a dithered 16-grey panel may simply not be recognisable, in which case art-history study belongs on the tablet and the e-ink device stays for text. Recording that is the finding; a criterion that can only be satisfied by success would hide it.
- 2026-08-22 09:00: The reported dark-mode defect is recorded as a **candidate** diagnosis, not a confirmed one. Reading `study_answer_partial.html:36` and `base.html:23-25` explains both an unreadable back and silently unstyled lists, but a UI defect closes on a reproduction, not on code inspection. The reproduction is the first step of run 4, not a conclusion of this session.

- 2026-08-22 15:10: Migration numbered **016**, and the numbering was taken from the deployed `goose_db_version` rather than the files on disk — the rule 015 cost us. The deploy log of 2026-08-21 records `successfully migrated database to version: 15`, so 016 is the next free slot. The rehearsal staged a copy to 15 first and applied only 016 against it, which is the transition the live instance will actually perform; a straight `goose up` from 13 would have proven something else.
- 2026-08-22 15:10: **Learning and relearning cards are never buried**, which tightens ISC-45 from "every other card of that article that is currently due". The naive reading kills the feature that shipped two days earlier: a card failed minutes ago is due in five, ISC-74's learn-ahead window is what brings it back inside the session, and burying it until tomorrow would delete exactly that re-retrieval. Anki draws the same line — its interday-learning-sibling burying is off by default. Caught in design rather than by a failing probe, so a test locks it in place (`TestBurySiblingsLeavesLearningAndRelearningAlone`).
- 2026-08-22 15:10: The bury filter went into **every** count of due cards, not just the study queue — deck list, deck overview, dashboard `DueToday`, and the `due_only` card API. A "Study (5)" button that opens onto an empty session is a worse defect than no burying at all, because it breaks the number the learner uses to decide whether to sit down. Five sites, enumerated by one grep for `due <= ?`.
- 2026-08-22 15:10: Burying does not touch `updated_at`, not just the FSRS columns. Answering one card would otherwise restamp every sibling as edited, and `updated_at` is the field that says a human changed the content. Burying changes what the queue shows, and nothing else.
- 2026-08-22 15:10: Siblings are scoped by article and by **owner**, not by deck. A card the learner moved to another of their own decks is still generated from the same article and still primes the answer; a write path scoped only by article would reach across users. `BurySiblings` takes `userID` and filters on deck ownership, matching the `...ForUser` idiom the rest of `CardService` already uses.
- 2026-08-22 15:10: The day boundary is the next **local** midnight, resolved through `time.Local` — the same zone convention `recall metrics` established for its day boundaries and hour histogram. No new configuration: the container's `TZ` sets it, as it already does for metrics. `time.Date` with an overflowing day normalizes the month and resolves the offset in the zone, so the DST transition moves the boundary instead of breaking it (proven for Europe/Madrid's October change).
- 2026-08-22 15:10: The unbury control is a plain form POST that redirects, not an HTMX swap. It changes what the whole page says — the study count, the banner, the button — so re-rendering the page is the honest response, and it leaves the deck page working without JavaScript.

- 2026-08-23 12:00: Reaching the threshold **flags** the card; it does not suspend it. Anki auto-suspends at eight lapses, and this system does not, because the standing rule is that anything touching the learner's own cards proposes and waits. Auto-suspension silently removes material the learner may still need and gives no signal that it happened — the failure mode the leech mechanism exists to end, reproduced one level up.
- 2026-08-23 12:00: `LeechThreshold` and `IsLeech()` live in `models`, not `services`. The study templates ask a card whether it is a leech, and a template cannot reach into the services package; putting the number anywhere else would mean a second copy of the number in the template.
- 2026-08-23 12:00: Suspension went into **every** due count as well as the study queue — deck list, deck overview, dashboard `DueToday`, the `due_only` card API — the same five sites the bury filter went into a day earlier. Second instance of the same class in two days, so the sweep is now the default move for any column that hides a card.
- 2026-08-23 12:00: Suspended cards are **excluded from sibling burying and from the held-back count**. Burying a card that nothing serves writes a column no path reads, and counting it as "held back until tomorrow" over-reports the banner with cards that are not coming back tomorrow at all. The two hiding mechanisms have to know about each other or the numbers drift.
- 2026-08-23 12:00: Suspended leeches stay **on** the leech list. The list is where the learner decides what to do about a bad card, and a card taken out of rotation is still a card that needs rewriting or deleting — dropping it from the list would turn suspension into a way to forget the problem rather than park it.
- 2026-08-23 12:00: The empty leech list explains why it is empty. Zero is the expected reading for a while: a leech counter counts failures, and failures only started being registered when production cards shipped three days ago. A bare "nothing here" would read as "your cards are fine", which is precisely the false reassurance the refuted leech-detector conjecture in the Changelog is about.

- 2026-08-23 13:00: The tag vocabulary is governed by a standard written outside this repo — `LIFEOS/RULES/Tagging.md` — because the same drift recurs across the principal's other surfaces (the Knowledge Archive, Obsidian, Readeck) and a rule that lives in one project's ISA cannot govern the others. This ISA takes a dependency on it: ISC-54 and ISC-55 are restated in its terms.
- 2026-08-23 13:00: Recall's exposure to tag drift is **structurally near zero**, and noticing that is what unblocked the feature. ISC-55 already says tags are derived from the source article rather than typed, so no human types a tag here. Drift is a property of open vocabularies with human writers; a generator constrained to a closed root cannot produce `agents` one day and `agentes` the next. The standard matters most where the principal types — which is not this application.
- 2026-08-23 13:00: Tags are stored as a normalized `key` alongside the display form, not as one string. Two tags with the same key are the same tag, which is the whole of the orthographic fix, and it has to live in the schema rather than in a validator so that no write path can bypass it.

- 2026-08-23 16:00: A no-reschedule session writes **nothing at all**, not merely no FSRS columns. ISC-58 as written would have allowed a `review_logs` row, and that row would be worse than a scheduling write: `recall metrics` computes true retention from the review log, so logging cram passes would quietly corrupt the one instrument every outcome claim in this ISA is measured with. The criterion is widened rather than satisfied narrowly.
- 2026-08-23 16:00: The no-reschedule pass advances by a **cursor**, not by a set of seen cards. Nothing is written in that mode, so a card stays due and would be served forever; ordering by id and remembering the last one served turns the pass into one clean sweep and costs a single string of session state. A growing set of ids in a cookie is a session that breaks at some length nobody tested.
- 2026-08-23 16:00: Tagging is wired into `CreateBatch` itself rather than into the three generation call sites. The web handler, the API handler and the cron all create cards through it, so none of them can forget, and a fourth call site added later inherits the behaviour instead of needing to remember it.
- 2026-08-23 16:00: Classification happens **before** the transaction opens. It can be a network call, and a network call inside a write transaction holds the SQLite write lock for as long as the provider feels like taking.
- 2026-08-23 16:00: One classification per article, not per card, and a sibling's existing tag is reused before the classifier is asked at all. The daily cron runs against the same articles repeatedly; re-classifying would cost a call each time and risk a different answer for one batch.
- 2026-08-23 16:00: A classification failure leaves the card **untagged and created**, never tagged wrongly. An invented domain is refused by `ValidateTag` before it reaches the database, and the backfill is the thing that exists to find the untagged ones later. The closed root is only closed if failure means "none" rather than "make one up".
- 2026-08-23 16:00: The uniqueness of a tag key lives in the **schema** — `UNIQUE(user_id, key)` — not in a validator. Two tags with the same key are the same tag, and no write path added later gets a vote on that.
- 2026-08-23 16:00: The filter travels in the session, not in the URL. The study partials are shared with deck sessions and build their URLs by path; threading a query string through each of them would change the markup a deck session serves, which ISC-42 forbids. The partials learned one optional `StudyBase` instead, which is empty for deck sessions and therefore renders exactly what it rendered before.

- 2026-08-23 18:00: The prompt's **example** was the heavier half of the fix. The old one demonstrated a multi-fact back inside `<ul><li>`, and a model copies the shape it is shown far more reliably than it follows a rule it is told. Removing the five list-formatting rules without replacing that example would have changed very little. The new example shows two atomic cards, in Spanish, one of them the reverse direction of the other, one marked production.
- 2026-08-23 18:00: `FlashcardPair` carries `kind`, and an unrecognised value falls back to recognition rather than being rejected. The generator now judges whether an answer is an arbitrary label; a future model that returns `cloze` or an empty string must not be able to change how a card is asked, and must not lose the card either.
- 2026-08-23 18:00: A split **suspends** the original rather than deleting it. The original is the operator's own writing, it carries months of scheduling history the new atomic cards do not have, and a suspension is reversible where a delete is not. This reuses the column ISC-62 added two days earlier; the leech list is where it can then be deleted deliberately.
- 2026-08-23 18:00: The splitter selects with `hasListMarkup` and `hasConjunction`, the same two detectors `recall metrics` counts ISC-65 with. Two implementations of "malformed" would drift apart, and the number being fixed would stop being the number being measured.
- 2026-08-23 18:00: Applying names **one card id**. There is no bulk apply, not even behind a flag. Anti-8 requires per-card confirmation, and a `--all` that existed would eventually be used at three in the morning.


- 2026-08-23 20:00: "Introduced today" is a card whose **first review** falls today, not a review log carrying state 0. The scheduler writes the state a card moved *into*, so a new card rated Good logs as learning; counting the log's state would have missed every card the new-card limit exists to count. Found by reading `scheduler.Schedule` before writing the query rather than after.
- 2026-08-23 20:00: The review limit counts **distinct cards**, not reviews. Four relearning attempts at one failed card are one card's worth of load, and counting them separately would spend the day's budget as a punishment for failing — the one thing this system must never do, since it exists to make failure registrable.
- 2026-08-23 20:00: Learning and relearning cards are exempt from both limits. A limit bounds how much load is taken on; a card mid-loop is load already taken on, and refusing to finish it strands exactly the re-retrieval ISC-51 and ISC-74 restored.
- 2026-08-23 20:00: Zero means none today, not unlimited. It is the honest reading of the number and it makes "no new cards while I catch up" expressible, which is the setting a learner with a backlog actually wants.
- 2026-08-23 20:00: The limits are per user, so `GetNextDue` now takes a user id. The signature change touches four call sites and is worth it: deriving the owner from the deck inside the query would have hidden the fact that the budget is global while the queue is per deck.

## Changelog

- **conjectured**: filtering the queue's state list by the day's remaining budget enforces the study limits — with two new cards left, allow state 0; with none, do not.
  **refuted by**: the browser, on a deck of five untouched new cards with the new-card limit set to two. The deck page read **Study (5)**, the dashboard read five due, and the queue's own count agreed. The filter was working exactly as written and the numbers were still wrong.
  **learned**: the state filter is a **boolean** — it decides *whether* new cards may be served at all, not *how many*. Serving was correct all along, because each grading raises the count and the filter closes on its own; counting was not, because a predicate that admits the class admits every card in it. A count has to cap each class by what remains of that class's budget, which is arithmetic the WHERE clause cannot do.
  **criterion now**: ISC-68 is widened from "enforced by the study queue" to "and every count of due cards is capped by the same budget". This is the third time in this run that a mechanism hid cards from the session while the counts kept promising them — buried, then suspended, now over-budget — so the count is computed once, in `CappedDueCount`, and every surface reads it.

- **conjectured**: after a split, the atomic cards can be found as "the cards in this deck that have no tag yet", so they can inherit the original's tags without threading ids through the transaction.
  **refuted by**: the test asserting the untouched cards stayed untouched — `compound` and `fine` were sitting in the same deck with no tags of their own, and the first cut attached the split card's topic to both of them.
  **learned**: "the rows I just created" is not a property any query over the table can recover after the fact, and a predicate that happens to select them today selects whatever else drifts into that shape tomorrow. The writer has to keep the ids it wrote.
  **criterion now**: unchanged in wording — ISC-66 already said the splitter writes nothing until the operator confirms *that specific card*, and the defect was a violation of it that the wording had already ruled out. The test that catches it is now the one that asserts non-named cards are untouched, rather than only asserting the named one changed.

- **conjectured**: an Anki-style `::` hierarchy of free depth expressed in the tag name is the right shape for the tag store (ISC-54 as originally written).
  **refuted by**: measuring the principal's existing vocabulary in the Knowledge Archive — 84 notes carrying 61 distinct tags, 29 of them used exactly once. The singletons are not obscure topics; they are the same topics at different depths and in two languages: `agents` beside `agentes`, `architecture` beside `arquitectura`, `deploy`/`docker`/`vps`/`ssh`/`infraestructura` for one subject, and `musica`/`guitarra`/`compositor`/`cancion`/`rock` flattened across five levels of granularity.
  **learned**: free depth is a drift generator, not a drift remedy. A hierarchy of unbounded depth offers infinitely many defensible places to file one idea, and every additional level multiplies them, so the `::` was going to reproduce inside Recall exactly the vocabulary the archive already demonstrates. Depth has to be fixed before hierarchy helps at all. Three distinct failure modes were being treated as one — orthographic, lexical and granularity drift — and each needs a different mechanism: a normalized key, an alias table, and a fixed depth respectively.
  **criterion now**: ISC-54 requires a normalized `key` plus a display form, at a fixed depth of two, with the first segment drawn from a closed list; ISC-55 forbids the generator from inventing a first segment. The reasoning and the audit tool live in `LIFEOS/RULES/Tagging.md`.

- **conjectured**: the unreadable green on a card back in dark mode is `study_answer_partial.html`'s `text-green-700 dark:text-white` losing its `dark:` override, and the list CSS in `base.html` keyed on `.dark .text-green-400 ul` means list markers vanish from card backs in dark mode too.
  **refuted by**: opening the answer view in dark mode in a real browser against a running instance — the back renders **white**, and a `<ul>` back renders with visible bullets and indentation. Both halves of the diagnosis are false for the current code.
  **learned**: the `dark:` variant does win (Tailwind orders variants after base utilities), and the list CSS matches because the element keeps its unprefixed `text-green-700` class in both themes. Reading the classes explained a defect that the running page does not have. This is the fourth time in this file that code inspection produced a confident wrong answer; the rule that a UI defect closes on a reproduction is not a formality.
  **criterion now**: run 4's palette work does not start from this diagnosis. The reported green is still real and still unlocated — twelve green utilities across the templates carry no `dark:` override at all — so the first step of run 4 is to have the principal name the screen, then reproduce it there.

- **conjectured**: a snippet anchored on the first match in the body is the useful excerpt.
  **refuted by**: the live `/search?q=acordes de septima` screenshot — the excerpt centred on "de" inside the Wikipedia nav chrome and said nothing about séptima chords, because "de" is the first term to appear.
  **learned**: match *position* is a bad anchor; match *density weighted by term length* is the right one, since long rare words carry the query's meaning and short common ones appear everywhere.
  **criterion now**: ISC-9 requires the snippet to centre on the densest match cluster weighted by matched length, not on the first match.

- **conjectured**: substring matching is fine for highlighting, since FTS5 already decided which rows match.
  **refuted by**: the same screenshot — "acor**de**" lit up mid-word throughout, so the highlighting carried no signal.
  **learned**: the highlighter must model the same token boundaries the FTS tokenizer uses; FTS5 matches whole tokens (prefix-extended on the last one), so a highlight must begin a word.
  **criterion now**: ISC-9 requires matches to be anchored at word starts.

- **conjectured**: a collection whose owner reports poor recall will show it in the data as a population of repeatedly-failed cards, so a leech counter is the high-value missing mechanism.
  **refuted by**: the operator's own database — zero cards at or above the leech threshold, and a maximum lapse count far below it, in a collection its owner describes as not sticking.
  **learned**: a leech detector counts failures, so it is blind in a system where failure is never registered. Self-graded recognition produces almost no `Again`, so no card ever accumulates lapses no matter how bad it is. Detection mechanisms are downstream of measurement mechanisms; ordering them the other way builds a counter that counts nothing.
  **criterion now**: leech detection (ISC-60…ISC-62) is retained but sequenced after production cards, and the credibility of the whole set is gated on ISC-71's retention band rather than on a leech count.

- **conjectured**: long, multi-fact card backs are harder, so they will show a higher failure rate than short ones, and back length will correlate with lapses.
  **refuted by**: measurement across the collection — cards with list-structured backs were failed no more often than cards without, and the shortest cards showed the *highest* rate of `Again`-or-`Hard`, the opposite of the predicted direction.
  **learned**: a card carrying five facts cannot be failed, only failed-partially, and no rating button expresses that — so the learner rates it as a partial success and the miss is never recorded. Short cards are the only ones whose outcome is binary, which is why they look harder. Card length does not raise the failure rate; it suppresses the failure *signal*, which is worse, because the scheduler then extends intervals on material that was not recalled.
  **criterion now**: atomicity is specified as an instrument requirement (Principles: an instrument that cannot register failure is not an instrument) and probed by ISC-63 and ISC-65, rather than being justified by a difficulty correlation that does not exist.

- **conjectured**: with fuzz enabled, two cards in identical state rated identically at the same instant will receive different due dates, so a batch generated together stops marching together.
  **refuted by**: reading the library before writing the test — `go-fsrs` seeds its PRNG from `fmt.Sprintf("%d_%d_%f", now.Unix(), reps, difficulty*stability)`. Identical state at an identical second is an identical seed, so the fuzz is identical too. The claim was not merely unproven, it was false.
  **learned**: fuzz disperses along the axes that actually differ between cards — review second, repetition count, and difficulty × stability — not along card identity. Two cards genuinely alike in all three are indistinguishable to the scheduler by construction, and after a card's first review no two cards stay alike in all three. The claim had assumed a mechanism the library never offered.
  **criterion now**: ISC-50 asserts dispersion of the interval itself — a sweep of review seconds over one card state yields more than one distinct interval, all inside the library's documented `FUZZ_RANGES`, where the same sweep with fuzz off collapses to exactly one.

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

**ISC-29 — closed 2026-08-19, real browser.** The 2026-08-17 deferral misdiagnosed the cause. The pinned context `interceptor-test` was configured correctly all along; the blocker was a stale `interceptor-daemon` (pid 82315, up 4d20h, from before the 0.22.37 upgrade) squatting on port 19222, so the new CLI could neither reuse it nor start its own. `pkill -f interceptor-daemon` cleared it and the isolation gate passed on the next run.

Four states verified, two of them in viewed pixels:
1. Default: field empty, helper reads "Currently generating with `openai/gpt-oss-120b`". Pixels viewed.
2. Typed `openai/gpt-oss-20b` into the field and clicked Save Settings through the real UI. Pixels viewed after reload: input shows `openai/gpt-oss-20b`, helper reads "Currently generating with `openai/gpt-oss-20b`".
3. Cleared the field and saved: value empty, helper back to `openai/gpt-oss-120b`. State claim, checked by DOM read; its appearance is state 1, already seen.

**Finding — the placeholder reads as a value.** `placeholder="openai/gpt-oss-120b"` renders dark enough in this theme that a viewed screenshot cannot distinguish "empty, following the instance default" from "explicitly pinned to gpt-oss-120b". Those two states mean different things: the second survives a change to `LLM_MODEL`, the first does not. Caught only because a DOM read contradicted what the pixels suggested. Not fixed this run.

### Code audit — 2026-08-20

The Problem section's claims were read out of the source this run, not recalled:

- `internal/scheduler/scheduler.go` — `params := fsrs.DefaultParam()` followed by `params.EnableShortTerm = false`; nothing else is overridden.
- `go-fsrs` v3.3.1 `parameters.go` — `DefaultParam()` sets `RequestRetention: 0.9`, `MaximumInterval: 36500`, `EnableShortTerm: true`, `EnableFuzz: false`. Recall therefore runs with fuzz off and short-term forced off.
- `go-fsrs` v3.3.1 `fsrs_test.go` `TestLongTermScheduler` — with `EnableShortTerm = false`, the documented interval sequence for `Good`×6 then `Again`, `Again` is `{3, 11, 35, 101, 269, 669, 12, 2}`: a failed card at a 669-day interval returns in 12 days, and a second failure in 2. No sub-day interval exists in that mode.
- `internal/services/review.go` `GetNextDue` — scoped to one `deck_id`, ordered `CASE WHEN state IN (0,1) THEN 0 ELSE 1 END ASC, due ASC`, with no limit, no bury filter and no suspend filter.
- `migrations/001_init.sql` — `cards` carries `deck_id`, `front`, `back` and the FSRS columns only; no `tags`, `suspended`, `buried_until` or flag column exists in any migration through 013.
- `internal/services/llm.go` — `DefaultFlashcardPrompt` contains five consecutive formatting rules mandating `<ul><li>` / `<ol><li>` structure and no atomicity rule.
- `internal/services/cron.go` — a whole batch of cards per article is generated in one call and inserted via `CreateBatch`, so every card of an article shares a creation time and an initial due date.
- `cmd/recall/main.go` — the only study route is `auth.GET("/decks/:id/study", …)`; there is no cross-deck or filtered study path.
- `daily_card_limit` is read only by `internal/services/cron.go` and the profile/account handlers — it bounds generation, never the study queue.

**Repository visibility, checked this run.** `GET https://api.github.com/repos/deleyva/recall` → 200 with `"private": false, "visibility": "public"`, and `git ls-files --error-unmatch ISA.md` confirms this file is tracked. That is why no measured operator behaviour appears anywhere above.

### metrics-command — 2026-08-20

**ISC-36 — the eight measures (nine with the followup matrix).** `internal/services/metrics.go` computes them; `recall metrics <email> [--json] [--tz <Zone>]` prints them. Fourteen tests in `internal/services/metrics_test.go` assert each measure against a hand-worked fixture, and every expected value is derived in a comment above its test so a failure points at the metric rather than at the fixture:

```
$ go test ./internal/services/ -run TestMetrics -v
--- PASS: TestMetricsCorpusIsUserScoped
--- PASS: TestMetricsTrueRetentionCountsOnlySpacedReviews
--- PASS: TestMetricsRatingDistribution
--- PASS: TestMetricsRatingFollowupPredictsNextAgain
--- PASS: TestMetricsLeeches
--- PASS: TestMetricsSiblingSameDayShare
--- PASS: TestMetricsFormulationMarkers
--- PASS: TestMetricsDeckDistributionIsOrdered
--- PASS: TestMetricsHourHistogramUsesRequestedZone
--- PASS: TestMetricsHourHistogramShiftsWithZone
--- PASS: TestMetricsRenderIsByteIdenticalAcrossRuns
--- PASS: TestMetricsJSONIsByteIdenticalAcrossRuns
--- PASS: TestMetricsUnknownUserIsAnError
--- PASS: TestMetricsEmptyCorpusDoesNotPanic
ok  github.com/deleyva/recall/internal/services
```

Red before green: the same command against the test file with no implementation present failed to build with `undefined: Metrics`, `undefined: NewMetricsService`, `undefined: RatingFollowup`, `undefined: LeechThreshold`, `undefined: RenderMetrics`.

User scoping is asserted rather than assumed — the fixture gives a second user a leech card with a list back and a compound front, and every count is checked to exclude it.

**ISC-37 — byte identity.** Run twice against an unchanged populated copy of a real database, output redirected to two files:

```
$ cmp /tmp/p1.txt /tmp/p2.txt && echo IDENTICAL
IDENTICAL (2370 bytes)
```

Two tests cover the same property at the unit level, for the text report and for the JSON form. The determinism is structural, not incidental: no timestamp is written into the report, the deck list is explicitly sorted by count then name, the hour histogram is a fixed 0..23 slice, and the rating rows are emitted by iterating 1..4 rather than a map.

**Regression.** `go build ./...`, `go vet ./...` clean; `go test ./...` green across the API and services suites. `gofmt -l` reports no drift in the files this work touched.

### production-cards — 2026-08-20

**ISC-38 / Anti-9 — migration on a populated copy.** Rehearsed against a copy of the live database (477 cards), not a fresh one:

```
version before: 14 · cards: 477
OK   015_card_kind.sql (442.38µs)
goose: successfully migrated database to version: 15
$ cmp before.txt after.txt      # all FSRS columns, all 477 cards
IDENTICAL
$ sqlite3 copy.db "SELECT kind, COUNT(*) FROM cards GROUP BY kind;"
recognition|477
$ sqlite3 copy.db "SELECT (SELECT COUNT(*) FROM search_index WHERE kind='flashcard'), (SELECT COUNT(*) FROM cards);"
477|477
```

Every existing card defaults to recognition, no FSRS column moved, and the search index stayed one-to-one.

**ISC-39 / ISC-41 / ISC-43 — real browser, real session** (local server, isolated Chrome profile via Interceptor):

- ISC-39: on a production card's study page, `document.documentElement.innerHTML.includes('<the answer>')` → `false`, with the answer input present in the tree. The answer is not in the page at all.
- ISC-41 miss: typed a wrong answer → screenshot shows the "Not produced" badge, the typed text, a word-level comparison (matched words green, the missing word underlined, the extra word struck through), and **Again carrying the pre-selection ring**, with all four grades still clickable.
- ISC-41 hit: typed the answer without accents → "Produced" badge, comparison rendered in the stored spelling, and **Good carrying the ring**.
- ISC-43: from the authenticated page, `fetch('/decks/…/study/<unattempted card>/answer')` → `403 | body: Type the answer before revealing this card. | leaks answer: false`.

**ISC-40 / ISC-42 / ISC-70 — tests.** `internal/services/answercheck_test.go` covers the comparison across case, accents, punctuation, inner whitespace, stored HTML and entities, plus wrong/missing/extra/empty answers, the suggested rating either way, the diff statuses, and the stored-spelling rule. `internal/handlers/web/review_test.go` drives the handlers: the production view withholds the answer, the recognition view is unchanged (reveal button, no input, answer route still 200 with the answer), both bypass routes 403, the pre-selection ring appears on the right button and only there, the reveal is scoped to the attempted card and cleared by grading, and switching a card's kind moves no FSRS column and is refused for another user's card or an unknown value.

```
$ go test ./... 
ok  github.com/deleyva/recall/internal/handlers/api
ok  github.com/deleyva/recall/internal/handlers/web
ok  github.com/deleyva/recall/internal/services
```

Red before green on both files: `undefined: CheckAnswer`, `undefined: RatingGood`, `undefined: RatingAgain` for the comparison, and the handler suite failing on the guard before it was fixed.

**Regression.** `go build ./...` and `go vet ./...` clean, full suite green, and `gofmt` reports no drift introduced by these files.

### scheduler-honesty — 2026-08-21

**Red before green.** With the flags as they were, the new scheduler suite failed with the defect stated in its own output:

```
--- FAIL: TestFailingAReviewCardReturnsItWithinTheDay
    Again scheduled the card 120h0m0s out, want a positive gap under 24h
    state = 2, want relearning (3)
--- FAIL: TestPreviewAgreesWithTheScheduler
    Again previews "5d" — a day-scale interval means short-term scheduling is off
--- FAIL: TestNewCardGraduatesAfterTwoGoods
    after one Good state = 2, want learning (1)
--- FAIL: TestFuzzDispersesLongIntervals
    every interval came out identical ([179 179 179 … 179]) — fuzz is not applied
```

**ISC-50 / ISC-51 / ISC-75 — six scheduler tests green** after the change: a failed review card returns in minutes and in Relearning with its lapse counted, the button preview agrees with what the button does, a new card graduates to Review on the second Good with a multi-day interval, fuzz yields multiple distinct intervals all inside `FUZZ_RANGES`, the same sweep with fuzz off collapses to one, and constructing a scheduler alters no card.

**ISC-74 — seven queue tests green**: a card due in three minutes is served; one beyond the window is not; a review card due in five minutes is never pulled forward; a card mid-loop precedes new cards; new still precedes review; a card actually due beats one merely looked ahead to; an empty queue stays empty. The count and the selection share one predicate, so a served card is never reported as "0 remaining".

**ISC-52 — end to end in a real browser**, isolated profile, two review cards seeded at a 60-day interval:

1. First card revealed — the rating row read `Again 5m · Hard 4mo · Good 13mo · Easy 38mo`. Before this change the same card previewed `Again 5d`.
2. Pressed Again. The session moved on to the second card rather than ending.
3. Graded the second card Good. The session served **the failed card again**, screenshotted at "1 remaining".
4. Database after: the failed card is `state 3` (Relearning), `lapses 1`, `scheduled_days 0`, due in 4 minutes. The second card sits at 380 days.

**ISC-53 — nothing rewritten.** All FSRS columns of a populated copy (477 cards) dumped before and after starting the server on the new configuration:

```
$ cmp pre-restart.txt post-restart.txt
IDENTICAL (477 cards)
```

Existing cards keep the schedule they earned; the new behaviour applies from each card's next review.

**Regression.** `go build ./...`, `go vet ./...` clean; all four test packages green.

### Deploy — 2026-08-21

Pushed to `main`; CI ran `go vet ./... && go test ./...`, built and published
`ghcr.io/deleyva/recall:latest`; the stack was redeployed with `pullImage`.

- Pre-deploy backup of the live database taken first (schema 14, 477 cards, 1876 reviews).
- Container up on the new image, and the startup log shows the schema move: `goose: successfully migrated database to version: 15` followed by `Recall starting on :8080`. Migration 015 had already been rehearsed on a copy of this database, FSRS columns unchanged.
- Live probes: `/api/v1/health` → 200 `{"api":"v1","status":"ok"}`; an unauthenticated `/api/v1/search` → 401.
- Live behaviour, in a real browser against the deployed instance: opening a study session and revealing a card shows the rating row reading `Again 5m`. Before this deploy the same row read in days. Nothing was graded — revealing does not touch scheduling.
- The shipped binary carries the new command: `recall metrics` run inside the deployed container returns the same figures as the local run against a copy, which is also the pre-change baseline the outcome criteria (ISC-49, ISC-65, ISC-71, ISC-72) will be measured against.

Note on the four criteria stated as measured bands: they are deliberately not
closed here. They compare a window of behaviour after the change against the
baseline above, and there is no such window yet.

### bury-siblings — 2026-08-22

**ISC-44 / Anti-9 — migration on a populated copy.** A copy of a real populated
database was staged to version 15 — the version the deployed instance reports —
so the rehearsal performs the same transition the live instance will:

```
start version: 13
staged to version: 15
after up: 16
after second up: 16          (idempotent)
buried_until NULL on 11 of 11 existing cards
PRAGMA table_info(cards) → 17|buried_until|TEXT|0||0
index → idx_cards_buried_until
```

FSRS columns snapshotted before and after the 015→016 step and diffed:
`IDENTICAL — no FSRS column moved`. The copy carries 11 cards, so this proves
the mechanism, not scale; the production-copy rehearsal is still owed at deploy
time under the Constraint, and the startup log is the live confirmation.

**ISC-45 / ISC-46 / ISC-47 / ISC-48 — Go tests.** Nine tests across
`internal/services/bury_test.go` and `internal/handlers/web/bury_test.go`:
burying hides due siblings and not the answered card; learning and relearning
siblings are left alone; burying stops at the article, at not-yet-due cards, and
at the owner, while reaching a sibling the learner moved to another of their own
decks; a full FSRS-column snapshot is identical across a bury; the queue skips a
buried card and serves it again once the timestamp passes; unbury is scoped to
the deck and the owner and the cards are served again; the day boundary is local
midnight across month ends and the Europe/Madrid DST change. `go test ./...`
green, `go vet ./...` clean, `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build`
still produces the binary.

**ISC-45 / ISC-46 / ISC-48 — end to end in a real browser.** A local instance on
a copy of the database, three cards generated from one article, all due, driven
through Interceptor:

- Deck page before: `Study (3)`, no held-back banner.
- Studied the first card, rated Good → the session answered **All done!** rather
  than serving the primed sibling next.
- Database after that one rating: the answered card `buried_until` NULL, both
  siblings `2026-08-22T22:00:00Z` — 2026-08-23 00:00 in Europe/Madrid, the local
  boundary, not a fixed 24 hours.
- Deck page after: `Study` button gone, banner reads *"2 held back until
  tomorrow — they come from articles you already studied today"* with a
  **Study them now** control. Screenshot viewed in light and dark themes.
- Clicked it → redirect to the deck page, `Study (2)`, both `buried_until` back
  to NULL, and reopening the session served `SIBLING TWO`.

**Not closed here.** ISC-49 is a 30-day measurement of the same-day-sibling share
against a target under 20%. There is no window yet, and the mechanism shipping is
not the outcome.

### leech-detection — 2026-08-23

**ISC-62 / Anti-9 — migration on a populated copy.** Copy staged to version 16 —
the version the instance will be on once burying ships — then 017 rehearsed
alone against it:

```
staged to version: 16
after up: 17
after second up: 17 (idempotent)
suspended = 0 on 11 of 11 existing cards
PRAGMA table_info(cards) → 18|suspended|INTEGER|1|0|0
```

FSRS columns snapshotted across the 016→017 step and diffed:
`IDENTICAL — no FSRS column moved`.

**ISC-60 / ISC-61 / ISC-62 — Go tests.** Eleven tests across
`internal/services/leech_test.go` and `internal/handlers/web/leech_test.go`: the
threshold is a boundary (7 is not a leech, 8 is); the list is owner-scoped,
worst first, and includes suspended leeches; a suspended card is served by
nothing and comes back with an identical FSRS snapshot; suspension leaves every
due count (deck overview, deck list, dashboard, due-only API) while the leech
count still reports it; suspended siblings are neither buried nor counted as
held back; suspend and delete both stop at the owner; the list renders all three
remedies and omits cards under the threshold; the empty state explains itself;
the dashboard routes to the list either way; the flag renders on the question
and on the answer.

**ISC-60 / ISC-61 / ISC-62 — end to end in a real browser.** Local instance on a
copy, one card seeded at 11 lapses and one at 3, driven through Interceptor in
dark mode:

- Dashboard: *"1 card keeps failing — it is probably asking too much at once"*
  with a **Review them →** route. (The first render read "…they are probably
  asking…" for a single card; copy corrected and re-verified.)
- `/leeches`: the 11-lapse card with its deck name and an `11 lapses` badge,
  offering Edit, Suspend and Delete. The 3-lapse card is absent. Screenshot
  viewed.
- Study session: the card carries *"Leech — failed 11 times. Worth rewriting."*
  above the question.
- Clicked Suspend → the row reads *"Suspended — not being served"*, the button
  becomes **Put it back**, and the study session drops from 2 cards to 1,
  serving the other card instead. The dashboard reads `Due Today 1`, the deck
  reads `Study (1)`.
- Its FSRS row after suspension is byte-identical to before —
  `due 2026-08-23T11:44:24Z, stability 1.2, difficulty 8.4, reps 19, state 2`.

**A note on what this measures.** The threshold count will read zero on the real
collection for a while, and that is the correct reading rather than a bug: a
leech counter counts failures, and failure has only been registered since
production cards shipped.

### tags-and-filtered-study — 2026-08-23

**ISC-54 / Anti-9 — migration on a populated copy.** Copy staged to version 17,
then 018 rehearsed alone against it:

```
staged to version: 17
after up: 18
after second up: 18 (idempotent)
tags: 0, card_tags: 0, existing cards untouched: 11
duplicate key refused by the schema:
  UNIQUE constraint failed: tags.user_id, tags.key (2067)
```

FSRS columns snapshotted across the 017→018 step: `IDENTICAL — no FSRS column
moved`. The last line is the point of the design: the orthographic fix is a
schema constraint, and the rehearsal proves it bites rather than trusting the
validator above it.

**ISC-54…ISC-59 — Go tests.** Seventeen tests across
`internal/services/tag_test.go` and `internal/handlers/web/filtered_test.go`.
Five spellings of one tag normalize to one key; the shape is enforced and an
unknown domain is refused before it reaches the database; three spellings
converge on one row while the accented display form survives; vocabularies are
per user; attaching is idempotent. Generated cards are tagged at creation; a
second batch from the same article reuses the tag and does not re-classify; an
out-of-list domain leaves the cards created and untagged rather than tagged
wrongly; the classifier is shown the temas already in use. The backfill dry run
writes nothing, `--apply` tags every card with a source article, cards without
one are reported by id, and a second pass finds nothing to do. A filtered session
selects by tag, by lapse floor, or by both, spans decks, stops at the owner, and
still honours suspended and buried cards.

**ISC-58 / ISC-59 — end to end in a real browser.** Local instance on a copy,
three tagged cards across two decks, driven through Interceptor in dark mode.

The picker offers the vocabulary with card counts — `Música/Armonía (2)`,
`Humanidades/Ilustración (1)` — a lapse floor, and the no-reschedule choice.
Screenshot viewed.

No-reschedule pass over `musica/armonia`, both cards revealed and graded Good
through the interface, session ending on **All done!**:

```
FSRS DIFF across a full no-reschedule session
  IDENTICAL — the schedule survived a graded pass
review logs   before: 18   after: 18
```

The same filter in normal mode, one card graded Good:

```
k1  due 2026-08-23 → 2026-09-26   stability 4.0 → 36.85   reps 8 → 9
k2, k3 unchanged
review logs 18 → 19
```

**Not closed here.** Nothing. ISC-54…ISC-59 are complete; the vocabulary itself
starts empty on the real collection and fills as `recall backfill-tags` and the
generator run.

### atomic-generation — 2026-08-23

**ISC-63 — the prompt.** `DefaultFlashcardPrompt` now opens on minimum
information and states what follows from it: N items become N cards, no
coordinating conjunction in a question, the shortest correct answer, `<strong>`
and `<em>` only with `<ul>`/`<ol>`/`<li>` forbidden outright, and both
directions for named things. The five consecutive HTML-list rules are gone, and
so is the JSON example that demonstrated a multi-fact `<ul><li>` back. A test
asserts both halves: the principle is stated, and none of the four old list
instructions survives anywhere in the template.

**ISC-64 — carried but not yet observed.** `FlashcardPair` gained `kind`, the
parser reads it, and `CreateBatch` writes it, with anything unrecognised falling
back to recognition — proven by a table test over production, recognition,
absent and invented values. What is not proven is what a live generation emits,
which is what the criterion is actually about. `[DEFERRED-VERIFY]`, closing on a
generation run against the deployed instance.

**ISC-66 / ISC-67 — the splitter.** Nine tests. The candidate list is exactly the
malformed cards and nothing else, keyed on the same two detectors `recall
metrics` measures with. A dry run leaves the card table byte-identical, proven
by a SHA-256 over every column of every row before and after a full propose
pass. Applying names one card: it becomes its atomic parts, the original is
suspended with its history intact rather than deleted, the atomic cards inherit
its tags, cards the operator did not name are untouched and pick up nothing, an
empty proposal is refused rather than suspending the original and leaving
neither card, and another user's card cannot be split.

Run against a copy of a real database with no LLM key, so the proposals fail and
the listing has to stand on its own:

```
CARDS THAT ASK FOR MORE THAN ONE THING — 4
DRY RUN. Nothing is written. Confirm one card at a time:
  recall split-cards <email> --apply <cardID>

cards before: 11:2701
cards after:  11:2701
CARD TABLE UNCHANGED by the dry run
```

**Not closed here.** ISC-65 is a 30-day measurement over cards generated after
the prompt change, and no such card exists yet.

### study-limits — 2026-08-23

**ISC-68 / Anti-9 — migration on a populated copy.** Copy staged to 18, then 019
rehearsed alone:

```
staged to version: 18
after up: 19
after second up: 19 (idempotent)
existing user: generation=5 new=20 reviews=200
```

The existing generation setting is untouched and the two study limits arrive at
Anki's defaults. FSRS columns across the step: `IDENTICAL`.

**ISC-68 — Go tests.** Eight tests. The new-card limit stops the queue
introducing more and raising it by one releases exactly one card; the review
limit is enforced independently, so spending one budget leaves the other
working; both limits at zero still serve learning and relearning cards and
nothing else; "introduced today" counts first reviews, so a card that logged as
*learning* is still counted as introduced; four relearning attempts at one card
spend one card's budget; the filtered cross-deck session obeys the same budget;
and the deck overview, the deck list, the dashboard and the queue all report the
same capped number.

**ISC-69 — the three settings.** Two API tests: writing each of the three limits
in turn leaves the other two exactly where they were, and each is validated on
its own terms — zero is a real answer for a study limit and a refusal for the
generation limit, and a refused write moves nothing.

In the browser, the profile page carries three distinct inputs with three
distinct labels and three distinct ranges:

```
daily_card_limit   | Cards generated per day | value 5   | min 1 max 20
daily_new_limit    | New cards per day       | value 20  | min 0 max 500
daily_review_limit | Reviews per day         | value 200 | min 0 max 9999
```

Submitting the form with only the new-card limit changed moved only that one:
`5/20/200` became `5/3/200`.

**ISC-68 — end to end.** A deck of five untouched new cards with the limit set
to two. The deck page reads **Study (2)**, the dashboard reads **2** due today
against 5 total. Six grading attempts driven through the interface introduced
exactly two distinct cards, `nueva 1` and `nueva 2`; the other three were never
offered.

The full-page screenshot of the profile was rendered too narrow to read, so the
UI half of ISC-69 closed on a targeted DOM read of those three fields rather
than on pixels. Stated rather than glossed: nothing here is an appearance claim.

### Remaining criteria

ISC-44…ISC-49, ISC-54…ISC-69 and ISC-71…ISC-73, plus Anti-7/5/7/8/9, are unbuilt. No evidence exists for them and none is claimed.
