package middleware

import (
	"net/http"

	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const (
	SessionName = "recall-session"
	UserIDKey   = "user_id"
	EmailKey    = "email"
)

type AuthMiddleware struct {
	store sessions.Store
}

func NewAuthMiddleware(store sessions.Store) *AuthMiddleware {
	return &AuthMiddleware{store: store}
}

func (m *AuthMiddleware) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		sess, err := m.store.Get(c.Request(), SessionName)
		if err != nil || sess.Values[UserIDKey] == nil {
			// Check if this is an API request
			if isAPIRequest(c) {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			return c.Redirect(http.StatusSeeOther, "/login")
		}
		userID, ok := sess.Values[UserIDKey].(string)
		if !ok {
			if isAPIRequest(c) {
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			}
			return c.Redirect(http.StatusSeeOther, "/login")
		}
		email, _ := sess.Values[EmailKey].(string)
		c.Set(UserIDKey, userID)
		c.Set(EmailKey, email)
		return next(c)
	}
}

func (m *AuthMiddleware) GetStore() sessions.Store {
	return m.store
}

func GetUserID(c echo.Context) string {
	if id, ok := c.Get(UserIDKey).(string); ok {
		return id
	}
	return ""
}

func isAPIRequest(c echo.Context) bool {
	return len(c.Path()) >= 4 && c.Path()[:4] == "/api"
}
