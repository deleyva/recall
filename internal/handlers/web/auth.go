package web

import (
	"net/http"

	"github.com/deleyva/recall/internal/handlers/middleware"
	"github.com/deleyva/recall/internal/services"
	"github.com/deleyva/recall/internal/templates"
	"github.com/gorilla/sessions"
	"github.com/labstack/echo/v4"
)

type AuthHandler struct {
	auth  *services.AuthService
	store sessions.Store
	tmpl  *templates.Registry
}

func NewAuthHandler(auth *services.AuthService, store sessions.Store, tmpl *templates.Registry) *AuthHandler {
	return &AuthHandler{auth: auth, store: store, tmpl: tmpl}
}

func (h *AuthHandler) LoginPage(c echo.Context) error {
	return h.tmpl.ExecuteTemplate(c.Response(), "login.html", map[string]interface{}{
		"Error": c.QueryParam("error"),
	})
}

func (h *AuthHandler) Login(c echo.Context) error {
	email := c.FormValue("email")
	password := c.FormValue("password")

	user, err := h.auth.Login(email, password)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/login?error=Invalid+credentials")
	}

	sess, _ := h.store.Get(c.Request(), middleware.SessionName)
	sess.Values[middleware.UserIDKey] = user.ID
	sess.Values[middleware.EmailKey] = user.Email
	sess.Values[middleware.IsAdminKey] = user.IsAdmin
	sess.Save(c.Request(), c.Response())

	return c.Redirect(http.StatusSeeOther, "/")
}

func (h *AuthHandler) RegisterPage(c echo.Context) error {
	return h.tmpl.ExecuteTemplate(c.Response(), "register.html", map[string]interface{}{
		"Error": c.QueryParam("error"),
	})
}

func (h *AuthHandler) Register(c echo.Context) error {
	email := c.FormValue("email")
	password := c.FormValue("password")

	if email == "" || password == "" {
		return c.Redirect(http.StatusSeeOther, "/register?error=Email+and+password+required")
	}

	if len(password) < 8 {
		return c.Redirect(http.StatusSeeOther, "/register?error=Password+must+be+at+least+8+characters")
	}

	user, err := h.auth.Register(email, password)
	if err != nil {
		return c.Redirect(http.StatusSeeOther, "/register?error=Email+already+registered")
	}

	sess, _ := h.store.Get(c.Request(), middleware.SessionName)
	sess.Values[middleware.UserIDKey] = user.ID
	sess.Values[middleware.EmailKey] = user.Email
	sess.Save(c.Request(), c.Response())

	return c.Redirect(http.StatusSeeOther, "/")
}

func (h *AuthHandler) Logout(c echo.Context) error {
	sess, _ := h.store.Get(c.Request(), middleware.SessionName)
	sess.Options.MaxAge = -1
	sess.Save(c.Request(), c.Response())
	return c.Redirect(http.StatusSeeOther, "/login")
}
