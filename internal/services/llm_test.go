package services

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// newUsersDB builds the smallest table the resolver reads, so the test does not
// depend on the full migration chain.
func newUsersDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, llm_model TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatalf("create users: %v", err)
	}
	return db
}

// TestResolveModelPrecedence is the claim that matters: a model change must be
// possible at the least expensive layer available. The per-user column wins over
// the instance env var, which wins over the compiled constant — so retiring a
// model is a text field first, an env var second, and a rebuild never.
func TestResolveModelPrecedence(t *testing.T) {
	cases := []struct {
		name       string
		userModel  string // stored in users.llm_model ("" = unset)
		envDefault string // LLM_MODEL ("" = unset)
		userID     string
		want       string
	}{
		{"user column wins over env", "user/model", "env/model", "u1", "user/model"},
		{"user column wins over fallback", "user/model", "", "u1", "user/model"},
		{"env used when column empty", "", "env/model", "u1", "env/model"},
		{"fallback when both empty", "", "", "u1", FallbackModel},
		{"blank column is not a value", "   ", "env/model", "u1", "env/model"},
		{"blank env is not a value", "", "   ", "u1", FallbackModel},
		{"unknown user falls back to env", "", "env/model", "nobody", "env/model"},
		{"empty userID skips lookup", "user/model", "env/model", "", "env/model"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := newUsersDB(t)
			if _, err := db.Exec(`INSERT INTO users (id, llm_model) VALUES ('u1', ?)`, tc.userModel); err != nil {
				t.Fatalf("seed user: %v", err)
			}

			s := NewLLMService("key", "", tc.envDefault, db)
			if got := s.ResolveModel(tc.userID); got != tc.want {
				t.Errorf("ResolveModel(%q) = %q, want %q", tc.userID, got, tc.want)
			}
		})
	}
}

// A nil DB is the background/no-owner case; it must degrade to the instance
// default rather than panicking.
func TestResolveModelWithoutDB(t *testing.T) {
	if got := NewLLMService("key", "", "env/model", nil).ResolveModel("u1"); got != "env/model" {
		t.Errorf("nil db: got %q, want env/model", got)
	}
	if got := NewLLMService("key", "", "", nil).ResolveModel("u1"); got != FallbackModel {
		t.Errorf("nil db, no env: got %q, want %q", got, FallbackModel)
	}
}

// The endpoint follows the same rule as the model: configurable without a
// rebuild, because a provider moving its URL is the same class of failure as a
// provider retiring a model.
func TestAPIURLOverride(t *testing.T) {
	if got := NewLLMService("key", "", "", nil).apiURL; got != FallbackAPIURL {
		t.Errorf("unset: got %q, want %q", got, FallbackAPIURL)
	}
	if got := NewLLMService("key", "  ", "", nil).apiURL; got != FallbackAPIURL {
		t.Errorf("blank: got %q, want %q", got, FallbackAPIURL)
	}
	const custom = "https://openrouter.example/v1/chat/completions"
	if got := NewLLMService("key", custom, "", nil).apiURL; got != custom {
		t.Errorf("override: got %q, want %q", got, custom)
	}
}
