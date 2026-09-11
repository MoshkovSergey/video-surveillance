package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/auth"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type userDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	IsActive bool   `json:"is_active"`
}

type tokenResponse struct {
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	User         userDTO `json:"user"`
}

func newUserDTO(u *domain.User) userDTO {
	return userDTO{
		ID:       u.ID.String(),
		Username: u.Username,
		Role:     string(u.Role),
		IsActive: u.IsActive,
	}
}

// handleLogin проверяет учетные данные и выпускает пару токенов.
func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректное тело запроса"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "укажите имя пользователя и пароль"})
		return
	}

	user, err := h.userRepo.GetByUsername(r.Context(), req.Username)
	if err != nil {
		h.logger.Error("failed to get user by username", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "неверные учётные данные"})
		return
	}
	if !user.IsActive {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "учётная запись отключена"})
		return
	}

	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil {
		h.logger.Error("failed to verify password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "неверные учётные данные"})
		return
	}

	access, refresh, err := h.tokens.Issue(user.ID, user.Username, string(user.Role))
	if err != nil {
		h.logger.Error("failed to issue tokens", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		User:         newUserDTO(user),
	})
}

// handleRefresh выпускает новую пару токенов по refresh-токену.
func (h *Handler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректное тело запроса"})
		return
	}

	claims, err := h.tokens.Parse(req.RefreshToken, auth.TokenRefresh)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "недействительный refresh-токен"})
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "недействительный refresh-токен"})
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get user for refresh", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if user == nil || !user.IsActive {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "недействительный refresh-токен"})
		return
	}

	access, refresh, err := h.tokens.Issue(user.ID, user.Username, string(user.Role))
	if err != nil {
		h.logger.Error("failed to issue tokens", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		User:         newUserDTO(user),
	})
}

// handleMe возвращает профиль текущего пользователя.
func (h *Handler) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := claimsFromContext(r)
	if claims == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "требуется аутентификация"})
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "недействительный токен"})
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), userID)
	if err != nil {
		h.logger.Error("failed to get current user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "пользователь не найден"})
		return
	}

	writeJSON(w, http.StatusOK, newUserDTO(user))
}