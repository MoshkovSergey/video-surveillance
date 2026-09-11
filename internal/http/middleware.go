package httpapi

import (
	"context"
	"net/http"
	"strings"

	"gitverse.ru/cataclysm78/video-surveillance/internal/auth"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

type contextKey string

const claimsKey contextKey = "claims"

func claimsFromContext(r *http.Request) *auth.Claims {
	v := r.Context().Value(claimsKey)
	if v == nil {
		return nil
	}
	claims, ok := v.(*auth.Claims)
	if !ok {
		return nil
	}
	return claims
}

// isPublicPath перечисляет маршруты, доступные без токена.
func isPublicPath(path string) bool {
	switch path {
	case "/healthz",
		"/healthz/db",
		"/api/v1/auth/login",
		"/api/v1/auth/refresh":
		return true
	}
	return false
}

// authMiddleware проверяет Bearer-токен на всех защищённых маршрутах.
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Публичные маршруты проходят без проверки токена.
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		header := r.Header.Get("Authorization")
		if header == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "требуется аутентификация"})
			return
		}

		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "некорректный заголовок авторизации"})
			return
		}

		claims, err := h.tokens.Parse(parts[1], auth.TokenAccess)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "токен недействителен или просрочен"})
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuth оставляет маршрут доступному любому аутентифицированному пользователю.
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return next
}

// requireRoles ограничивает маршрут перечисленными ролями.
func (h *Handler) requireRoles(next http.HandlerFunc, roles ...domain.UserRole) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r)
		if claims == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "требуется аутентификация"})
			return
		}

		for _, role := range roles {
			if string(role) == claims.Role {
				next.ServeHTTP(w, r)
				return
			}
		}

		writeJSON(w, http.StatusForbidden, map[string]string{"error": "недостаточно прав"})
	})
}