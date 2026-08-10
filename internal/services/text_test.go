package services

import (
	"strings"
	"testing"
)

func TestFoldAndTokens(t *testing.T) {
	if got := Fold("Música ÑOÑA Über"); got != "musica nona uber" {
		t.Errorf("Fold = %q", got)
	}
	got := Tokens(`  "El canto"  gregoriano, el CANTO!  `)
	want := []string{"el", "canto", "gregoriano"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Tokens = %v, want %v", got, want)
	}
	if len(Tokens("")) != 0 || len(Tokens("*** ??? ---")) != 0 {
		t.Error("symbol-only queries should produce no terms")
	}
	if n := len(Tokens(strings.Repeat("palabra1 palabra2 palabra3 palabra4 ", 20))); n > maxTerms {
		t.Errorf("Tokens returned %d terms, capped at %d", n, maxTerms)
	}
}

func TestHighlightIsAccentInsensitiveAndEscapes(t *testing.T) {
	out := Highlight("La música & el <ritmo> de Música", Tokens("musica"))
	if strings.Count(out, "<mark>") != 2 {
		t.Errorf("expected both accented forms marked: %q", out)
	}
	if !strings.Contains(out, "&amp;") || !strings.Contains(out, "&lt;ritmo&gt;") {
		t.Errorf("input HTML not escaped: %q", out)
	}
	if !strings.Contains(out, "<mark>música</mark>") {
		t.Errorf("original accents not preserved inside the mark: %q", out)
	}
}

func TestHighlightWithNoTermsIsJustEscaped(t *testing.T) {
	out := Highlight("<b>hola</b>", nil)
	if out != "&lt;b&gt;hola&lt;/b&gt;" {
		t.Errorf("got %q", out)
	}
}

func TestSnippetCentresOnMatch(t *testing.T) {
	body := strings.Repeat("relleno ", 200) + "la aguja escondida " + strings.Repeat("más relleno ", 200)
	snip := Snippet(body, Tokens("aguja"), 100)
	if !strings.Contains(snip, "<mark>aguja</mark>") {
		t.Fatalf("match not in snippet: %q", snip)
	}
	if !strings.HasPrefix(snip, "…") || !strings.HasSuffix(snip, "…") {
		t.Errorf("expected ellipses on both sides: %q", snip)
	}
	if n := len([]rune(snip)); n > 400 {
		t.Errorf("snippet too long: %d runes", n)
	}
}

func TestSnippetStripsTags(t *testing.T) {
	snip := Snippet("<ul><li>Forma <i>fugada</i></li></ul>", Tokens("fugada"), 100)
	if strings.Contains(snip, "<li>") || strings.Contains(snip, "<i>") {
		t.Errorf("tags survived: %q", snip)
	}
	if !strings.Contains(snip, "<mark>fugada</mark>") {
		t.Errorf("no mark: %q", snip)
	}
}

func TestStripHTML(t *testing.T) {
	if got := StripHTML("<p>uno</p>   <p>dos &amp; tres</p>"); got != "uno dos & tres" {
		t.Errorf("got %q", got)
	}
}

func TestSplitParagraphsUsesLineBreaksWhenPresent(t *testing.T) {
	got := SplitParagraphs("Primero.\n\nSegundo párrafo.\n  \nTercero.")
	if len(got) != 3 || got[1] != "Segundo párrafo." {
		t.Errorf("got %#v", got)
	}
}

func TestSplitParagraphsChunksFlatText(t *testing.T) {
	sentence := "Esta es una frase de prueba con unas cuantas palabras dentro. "
	got := SplitParagraphs(strings.TrimSpace(strings.Repeat(sentence, 60)))
	if len(got) < 2 {
		t.Fatalf("flat text was not chunked: %d paragraph(s)", len(got))
	}
	for i, p := range got {
		if len([]rune(p)) > 1200 {
			t.Errorf("paragraph %d too long: %d runes", i, len([]rune(p)))
		}
	}
	joined := strings.Join(got, " ")
	if !strings.HasPrefix(joined, "Esta es una frase") {
		t.Errorf("text mangled: %q", joined[:40])
	}
	if strings.Count(joined, "frase") != 60 {
		t.Errorf("sentences lost in chunking: %d of 60", strings.Count(joined, "frase"))
	}
}

func TestSplitParagraphsEmpty(t *testing.T) {
	if got := SplitParagraphs("   \n  "); got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}

func TestSnippetPrefersTheDenseMatchCluster(t *testing.T) {
	// "de" appears early in page chrome; the words that matter appear later.
	body := "Portal de la comunidad " + strings.Repeat("relleno ", 120) +
		"los acordes de séptima son acordes de cuatro notas"
	snip := Snippet(body, Tokens("acordes de septima"), 120)

	if !strings.Contains(snip, "<mark>acordes</mark>") || !strings.Contains(snip, "<mark>séptima</mark>") {
		t.Errorf("snippet missed the meaningful terms: %q", snip)
	}
	if strings.Contains(snip, "Portal") {
		t.Errorf("snippet anchored on the early stopword match: %q", snip)
	}
}

func TestHighlightOnlyMatchesAtWordStart(t *testing.T) {
	out := Highlight("un acorde de séptima", Tokens("de"))
	if strings.Contains(out, "acor<mark>de</mark>") {
		t.Errorf("matched inside a word: %q", out)
	}
	if !strings.Contains(out, "<mark>de</mark> séptima") {
		t.Errorf("standalone word not matched: %q", out)
	}
	// Prefix matches still work — they begin a word.
	if pre := Highlight("un acorde de séptima", Tokens("sépt")); !strings.Contains(pre, "<mark>sépt</mark>ima") {
		t.Errorf("prefix match lost: %q", pre)
	}
}
