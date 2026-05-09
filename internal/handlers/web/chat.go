package web

import (
	"log"
	"net/http"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/models"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/labstack/echo/v4"
)

type ChatHandler struct {
	articles *services.ArticleService
	chat     *services.ChatService
	gemini   *services.GeminiService
	tmpl     *templates.Registry
}

func NewChatHandler(articles *services.ArticleService, chat *services.ChatService, gemini *services.GeminiService, tmpl *templates.Registry) *ChatHandler {
	return &ChatHandler{
		articles: articles,
		chat:     chat,
		gemini:   gemini,
		tmpl:     tmpl,
	}
}

func (h *ChatHandler) ChatPage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	article, err := h.articles.Get(userID, articleID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Article+not+found")
	}

	messages, err := h.chat.ListByArticle(articleID, userID)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read?error=Could+not+load+chat")
	}

	return h.tmpl.ExecuteTemplate(c.Response(), "article_chat.html", map[string]interface{}{
		"Article":        article,
		"Messages":       messages,
		"Email":         c.Get(middleware.EmailKey),
		"IsAdmin":       middleware.IsAdmin(c),
		"GeminiEnabled":  h.gemini.IsConfigured(),
	})
}

func (h *ChatHandler) SendMessage(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")
	question := c.FormValue("message")

	if question == "" {
		return c.String(http.StatusBadRequest, "Message required")
	}

	if !h.gemini.IsConfigured() {
		return c.String(http.StatusServiceUnavailable, "Gemini API not configured")
	}

	// Get article for context
	article, err := h.articles.Get(userID, articleID)
	if err != nil {
		return c.String(http.StatusNotFound, "Article not found")
	}

	// Get existing chat history
	history, err := h.chat.ListByArticle(articleID, userID)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Could not load chat history")
	}

	// Save user message
	userMsg, err := h.chat.Create(articleID, userID, models.RoleUser, question)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Could not save message")
	}

	// Get AI response
	response, err := h.gemini.ChatWithArticle(article.Content, history, question)
	if err != nil {
		log.Printf("Gemini chat error for article %s: %v", articleID, err)
		return c.String(http.StatusInternalServerError, "Could not get AI response. Please try again.")
	}

	// Save assistant message
	assistantMsg, err := h.chat.Create(articleID, userID, models.RoleAssistant, response)
	if err != nil {
		return c.String(http.StatusInternalServerError, "Could not save response")
	}

	// Return both messages as HTML for HTMX append
	c.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(c.Response(), "chat_message_partial.html", map[string]interface{}{
		"Message": userMsg,
	}); err != nil {
		return err
	}
	return h.tmpl.ExecuteTemplate(c.Response(), "chat_message_partial.html", map[string]interface{}{
		"Message": assistantMsg,
	})
}

func (h *ChatHandler) ClearChat(c echo.Context) error {
	userID := middleware.GetUserID(c)
	articleID := c.Param("id")

	if err := h.chat.DeleteByArticle(articleID, userID); err != nil {
		return c.Redirect(http.StatusSeeOther, "/to-read/"+articleID+"/chat?error=Could+not+clear+chat")
	}

	return c.Redirect(http.StatusSeeOther, "/to-read/"+articleID+"/chat")
}
