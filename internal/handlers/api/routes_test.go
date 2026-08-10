package api

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

const specPath = "../../../static/openapi.yaml"

// testRouter registers the real route table against a throwaway Echo. The
// handlers are never invoked, so a zero-value Handler is enough.
func testRouter(auth echo.MiddlewareFunc) *echo.Echo {
	e := echo.New()
	if auth == nil {
		auth = func(next echo.HandlerFunc) echo.HandlerFunc { return next }
	}
	(&Handler{}).RegisterRoutes(e.Group("/api/v1"), auth)
	return e
}

// routerEndpoints returns "METHOD /path" strings in OpenAPI form: the /api/v1
// server prefix dropped and :params rewritten as {params}.
func routerEndpoints(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, r := range testRouter(nil).Routes() {
		if !strings.HasPrefix(r.Path, "/api/v1") || strings.Contains(r.Path, "*") {
			continue
		}
		path := strings.TrimPrefix(r.Path, "/api/v1")
		if path == "" {
			continue
		}
		for _, seg := range strings.Split(path, "/") {
			if strings.HasPrefix(seg, ":") {
				path = strings.Replace(path, seg, "{"+seg[1:]+"}", 1)
			}
		}
		out[r.Method+" "+path] = true
	}
	return out
}

var (
	specPathRe   = regexp.MustCompile(`^  (/\S*):\s*$`)
	specMethodRe = regexp.MustCompile(`^    (get|post|put|delete|patch):\s*$`)
)

// specEndpoints parses the paths section of the OpenAPI file. A hand-rolled
// scanner keeps the project free of a YAML dependency; the file is ours and its
// shape is fixed by this test.
func specEndpoints(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("open spec: %v", err)
	}
	defer f.Close()

	out := map[string]bool{}
	inPaths := false
	current := ""

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "paths:" {
			inPaths = true
			continue
		}
		if !inPaths {
			continue
		}
		if len(line) > 0 && line[0] != ' ' {
			break // a new top-level key ends the paths section
		}
		if m := specPathRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := specMethodRe.FindStringSubmatch(line); m != nil && current != "" {
			out[strings.ToUpper(m[1])+" "+current] = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan spec: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("parsed zero endpoints from the spec — the parser or the file shape changed")
	}
	return out
}

// ISC-24 — the spec and the router describe the same API, both directions.
func TestOpenAPISpecMatchesRouter(t *testing.T) {
	routes := routerEndpoints(t)
	spec := specEndpoints(t)

	var undocumented, phantom []string
	for r := range routes {
		if !spec[r] {
			undocumented = append(undocumented, r)
		}
	}
	for s := range spec {
		if !routes[s] {
			phantom = append(phantom, s)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(phantom)

	if len(undocumented) > 0 {
		t.Errorf("routes missing from static/openapi.yaml:\n  %s", strings.Join(undocumented, "\n  "))
	}
	if len(phantom) > 0 {
		t.Errorf("documented in static/openapi.yaml but not routed:\n  %s", strings.Join(phantom, "\n  "))
	}
	if len(routes) < 40 {
		t.Errorf("only %d routes registered — expected the full surface", len(routes))
	}
}

// ISC-25 — everything except health and the auth endpoints requires credentials.
func TestEveryPrivateRouteRequiresAuth(t *testing.T) {
	public := map[string]bool{
		"GET /health":         true,
		"POST /auth/register": true,
		"POST /auth/login":    true,
		"POST /auth/logout":   true,
	}

	// A stand-in for the real middleware: rejects anything without a Bearer token.
	guard := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !strings.HasPrefix(c.Request().Header.Get("Authorization"), "Bearer ") {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			return c.JSON(http.StatusOK, map[string]string{"ok": "reached handler"})
		}
	}

	e := testRouter(guard)
	for _, r := range e.Routes() {
		if strings.Contains(r.Path, "*") {
			continue
		}
		key := r.Method + " " + strings.TrimPrefix(r.Path, "/api/v1")
		if public[key] {
			continue
		}

		// Concrete path — :id segments replaced with a value.
		path := r.Path
		for _, seg := range strings.Split(path, "/") {
			if strings.HasPrefix(seg, ":") {
				path = strings.Replace(path, seg, "x", 1)
			}
		}

		req := httptest.NewRequest(r.Method, path, nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: got %d without credentials, want 401", r.Method, path, rec.Code)
		}
	}
}
