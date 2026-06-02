package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type ExplorerHandler struct {
	explorer *services.ExplorerService
	articles *services.ArticleService
	wikipedia *services.WikipediaService
	tmpl     *templates.Registry
}

func NewExplorerHandler(explorer *services.ExplorerService, articles *services.ArticleService, wikipedia *services.WikipediaService, tmpl *templates.Registry) *ExplorerHandler {
	return &ExplorerHandler{
		explorer:  explorer,
		articles:  articles,
		wikipedia: wikipedia,
		tmpl:      tmpl,
	}
}

func (h *ExplorerHandler) ListGoals(c echo.Context) error {
	userID := middleware.GetUserID(c)
	goals, err := h.explorer.ListGoals(userID)
	if err != nil {
		goals = nil
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "explore.html", map[string]interface{}{
		"Goals":   goals,
		"Email":   c.Get(middleware.EmailKey),
		"IsAdmin": middleware.IsAdmin(c),
		"Error":   c.QueryParam("error"),
		"Success": c.QueryParam("success"),
	})
}

func (h *ExplorerHandler) NewGoalPage(c echo.Context) error {
	return h.tmpl.ExecuteTemplate(c.Response(), "explore_new.html", map[string]interface{}{
		"Email":   c.Get(middleware.EmailKey),
		"IsAdmin": middleware.IsAdmin(c),
	})
}

func (h *ExplorerHandler) CreateGoal(c echo.Context) error {
	userID := middleware.GetUserID(c)
	title := c.FormValue("title")
	seedURL := c.FormValue("seed_url")
	timeHorizon, _ := strconv.Atoi(c.FormValue("time_horizon"))
	dailyPace, _ := strconv.Atoi(c.FormValue("daily_pace"))

	if seedURL == "" {
		return c.Redirect(http.StatusSeeOther, "/explore/new?error=Wikipedia+URL+required")
	}
	if timeHorizon <= 0 {
		timeHorizon = 14
	}
	if dailyPace <= 0 {
		dailyPace = 3
	}

	goal, err := h.explorer.CreateGoal(userID, title, seedURL, timeHorizon, dailyPace)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/explore/new?error="+err.Error())
	}

	return c.Redirect(http.StatusSeeOther, "/explore/"+goal.ID)
}

func (h *ExplorerHandler) GoalDetail(c echo.Context) error {
	userID := middleware.GetUserID(c)
	goalID := c.Param("id")

	goal, err := h.explorer.GetGoal(goalID, userID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/explore?error=Goal+not+found")
	}

	timeline := h.explorer.GetTimelineData(goalID, goal.TimeHorizon)

	return h.tmpl.ExecuteTemplate(c.Response(), "explore_detail.html", map[string]interface{}{
		"Goal":     goal,
		"Timeline": timeline,
		"Email":    c.Get(middleware.EmailKey),
		"IsAdmin":  middleware.IsAdmin(c),
	})
}

func (h *ExplorerHandler) DeleteGoal(c echo.Context) error {
	userID := middleware.GetUserID(c)
	goalID := c.Param("id")

	if err := h.explorer.DeleteGoal(goalID, userID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/explore?error=Could+not+delete+goal")
	}

	return c.Redirect(http.StatusSeeOther, "/explore?success=Goal+deleted")
}

func (h *ExplorerHandler) GraphJSON(c echo.Context) error {
	goalID := c.Param("id")

	data, err := h.explorer.GetGraphData(goalID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, data)
}

func (h *ExplorerHandler) NodePanel(c echo.Context) error {
	goalID := c.Param("id")
	nodeID := c.Param("nodeID")

	node, err := h.explorer.GetNode(goalID, nodeID)
	if err != nil {
		return c.String(http.StatusNotFound, "Node not found")
	}

	// Get goal for context
	userID := middleware.GetUserID(c)
	goal, _ := h.explorer.GetGoal(goalID, userID)

	return h.tmpl.ExecuteTemplate(c.Response(), "explore_node_panel.html", map[string]interface{}{
		"Node": node,
		"Goal": goal,
	})
}

func (h *ExplorerHandler) QueueNode(c echo.Context) error {
	goalID := c.Param("id")
	nodeID := c.Param("nodeID")
	day, _ := strconv.Atoi(c.FormValue("day"))

	if day <= 0 {
		day = 1
	}

	if err := h.explorer.QueueNode(goalID, nodeID, day); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	// Return updated node panel
	return h.NodePanel(c)
}

func (h *ExplorerHandler) UnqueueNode(c echo.Context) error {
	goalID := c.Param("id")
	nodeID := c.Param("nodeID")

	if err := h.explorer.UnqueueNode(goalID, nodeID); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	return h.NodePanel(c)
}

func (h *ExplorerHandler) AddToReadList(c echo.Context) error {
	userID := middleware.GetUserID(c)
	goalID := c.Param("id")
	nodeID := c.Param("nodeID")

	if err := h.explorer.AddNodeToReadList(goalID, nodeID, h.articles, userID); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}

	return h.NodePanel(c)
}

func (h *ExplorerHandler) ExpandNode(c echo.Context) error {
	goalID := c.Param("id")
	nodeID := c.Param("nodeID")

	newNodes, err := h.explorer.ExpandNode(goalID, nodeID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, newNodes)
}

func (h *ExplorerHandler) WikiSearch(c echo.Context) error {
	query := c.QueryParam("q")
	lang := c.QueryParam("lang")
	if query == "" {
		return c.JSON(http.StatusOK, []interface{}{})
	}
	if lang == "" {
		lang = "en"
	}

	results, err := h.wikipedia.SearchArticles(query, lang)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	// Convert to simpler JSON
	type searchResult struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
		URL     string `json:"url"`
	}
	var items []searchResult
	for _, r := range results {
		wikiURL := ""
		if len(r.Links) > 0 {
			wikiURL = r.Links[0]
		}
		items = append(items, searchResult{
			Title:   r.Title,
			Summary: r.Extract,
			URL:     wikiURL,
		})
	}

	data, _ := json.Marshal(items)
	return c.JSONBlob(http.StatusOK, data)
}

func (h *ExplorerHandler) AutoSchedule(c echo.Context) error {
	goalID := c.Param("id")
	userID := middleware.GetUserID(c)

	// Verify ownership
	_, err := h.explorer.GetGoal(goalID, userID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/explore?error=Goal+not+found")
	}

	if err := h.explorer.AutoSchedule(goalID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/explore/"+goalID+"?error="+err.Error())
	}

	return c.Redirect(http.StatusSeeOther, "/explore/"+goalID+"?success=Schedule+updated")
}
