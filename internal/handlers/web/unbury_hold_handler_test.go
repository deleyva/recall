package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// ISC-103 — "Study them now" has to mean the whole batch, today. The sequence
// the learner performs: unbury, answer one card, expect the next sibling.
// Before the hold, the answer buried the sibling again and the session ended
// on one card.
func TestUnburyThenAnswerServesTheNextSibling(t *testing.T) {
	h, db := newReviewHandler(t)
	seedSiblings(t, db)
	future := time.Now().UTC().Add(6 * time.Hour).Format(time.RFC3339)
	if _, err := db.Exec("UPDATE cards SET buried_until = ? WHERE id = ?", future, siblingB); err != nil {
		t.Fatalf("bury: %v", err)
	}

	c, _ := call(t, http.MethodPost, "/decks/"+testDeckID+"/unbury", url.Values{}, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	if err := h.UnburyDeck(c); err != nil {
		t.Fatalf("unbury: %v", err)
	}

	c, rec := call(t, http.MethodPost, "/decks/"+testDeckID+"/study",
		url.Values{"card_id": {siblingA}, "rating": {"3"}}, nil)
	c.SetParamNames("id")
	c.SetParamValues(testDeckID)
	if err := h.SubmitReview(c); err != nil {
		t.Fatalf("submit review: %v", err)
	}

	if buriedAt(t, db, siblingB) != nil {
		t.Errorf("the sibling was buried again by the first answer after the learner asked for the whole batch")
	}
	if strings.Contains(rec.Body.String(), "All done") {
		t.Errorf("the session ended after one card:\n%s", rec.Body.String())
	}
}
