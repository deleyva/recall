package middleware

import (
	"net/http"
	"strings"

	"github.com/deleyva/recall/internal/services"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

const (
	SessionName = "recall-session"
	UserIDKey   = "user_id"
	EmailKey    = "email"
)

type AuthMiddleware struct {
	store  sessions.Store
	tokens *services.TokenService
}

func NewAuthMiddleware(store sessions.Store) *AuthMiddleware {
	return &AuthMiddleware{store: store}
}

// SetTokenService sets the token service for Bearer auth. Called after initialization.
func (m *AuthMiddleware) SetTokenService(tokens *services.TokenService) {
	m.tokens = tokens
}

func (m *AuthMiddleware) RequireAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Try session auth first
		sess, err := m.store.Get(c.Request(), SessionName)
		if err == nil && sess.Values[UserIDKey] != nil {
			userID, ok := sess.Values[UserIDKey].(string)
			if ok {
				email, _ := sess.Values[EmailKey].(string)
				c.Set(UserIDKey, userID)
				c.Set(EmailKey, email)
				return next(c)
			}
		}

		// Try Bearer token auth for API requests
		if isAPIRequest(c) && m.tokens != nil {
			authHeader := c.Request().Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				userID, err := m.tokens.ValidateToken(token)
				if err == nil {
					c.Set(UserIDKey, userID)
					c.Set(EmailKey, "api-token")
					return next(c)
				}
				return c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			}
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}

		return c.Redirect(http.StatusSeeOther, "/login")
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
