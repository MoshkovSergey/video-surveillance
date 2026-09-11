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

// isPublicPath перечисляет маршруты, доступные без аутентификации.
func isPublicPath(path string) bool {
	switch path {
	case "/healthz", "/healthz/db", "/api/v1/auth/login", "/api/v1/auth/refresh":
		return true
	default:
		return false
	}
}

// authMiddleware проверяет Bearer-токен на всех защищенных маршрутах.
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing bearer token"})
			return
		}

		claims, err := h.tokens.Parse(strings.TrimPrefix(header, "Bearer "), auth.TokenAccess)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired token"})
			return
		}

		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// claimsFromContext извлекает claims текущего запроса.
func claimsFromContext(r *http.Request) *auth.Claims {
	claims, _ := r.Context().Value(claimsKey).(*auth.Claims)
	return claims
}

// requireAuth требует любую аутентифицированную роль.
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if claimsFromContext(r) == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		next(w, r)
	}
}

// requireRoles требует одну из перечисленных ролей.
func (h *Handler) requireRoles(next http.HandlerFunc, roles ...domain.UserRole) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims := claimsFromContext(r)
		if claims == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}

		for _, role := range roles {
			if string(role) == claims.Role {
				next(w, r)
				return
			}
		}

		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions"})
	}
}