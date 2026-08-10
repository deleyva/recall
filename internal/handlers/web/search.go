package web

import (
	"strconv"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type SearchHandler struct {
	search *services.SearchService
	tmpl   *templates.Registry
}

func NewSearchHandler(search *services.SearchService, tmpl *templates.Registry) *SearchHandler {
	return &SearchHandler{search: search, tmpl: tmpl}
}

// SearchPage renders the full search page. HTMX then swaps only the results.
func (h *SearchHandler) SearchPage(c echo.Context) error {
	data, err := h.results(c)
	if err != nil {
		return err
	}
	data["Email"] = c.Get(middleware.EmailKey)
	data["IsAdmin"] = middleware.IsAdmin(c)
	return h.tmpl.ExecuteTemplate(c.Response(), "search.html", data)
}

// SearchResults returns just the result list, for the live search box.
func (h *SearchHandler) SearchResults(c echo.Context) error {
	data, err := h.results(c)
	if err != nil {
		return err
	}
	return h.tmpl.ExecuteTemplate(c.Response(), "search_results_partial.html", data)
}

func (h *SearchHandler) results(c echo.Context) (map[string]interface{}, error) {
	userID := middleware.GetUserID(c)
	query := c.QueryParam("q")
	kind := c.QueryParam("kind")

	var kinds []string
	if kind != "" {
		kinds = []string{kind}
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit <= 0 {
		limit = 25
	}
	offset, _ := strconv.Atoi(c.QueryParam("offset"))

	results, total, err := h.search.Search(userID, query, kinds, limit, offset)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"Query":    query,
		"Kind":     kind,
		"Results":  results,
		"Total":    total,
		"Shown":    offset + len(results),
		"Searched": len(services.Tokens(query)) > 0,
		"Limit":    limit,
		"Offset":   offset,
	}, nil
}
