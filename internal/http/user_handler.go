package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"gitverse.ru/cataclysm78/video-surveillance/internal/auth"
	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type updateUserRequest struct {
	Username *string `json:"username"`
	Password *string `json:"password"`
	Role     *string `json:"role"`
	IsActive *bool   `json:"is_active"`
}

// handleListUsers возвращает список пользователей (только admin).
func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.List(r.Context())
	if err != nil {
		h.logger.Error("failed to list users", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	dto := make([]userDTO, 0, len(users))
	for i := range users {
		dto = append(dto, newUserDTO(&users[i]))
	}

	writeJSON(w, http.StatusOK, dto)
}

// handleCreateUser создает пользователя (только admin).
func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректное тело запроса"})
		return
	}

	if len(req.Username) < 3 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "имя пользователя должно содержать не менее 3 символов"})
		return
	}
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "пароль должен содержать не менее 8 символов"})
		return
	}

	role := domain.UserRole(req.Role)
	if !role.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректная роль"})
		return
	}

	existing, err := h.userRepo.GetByUsername(r.Context(), req.Username)
	if err != nil {
		h.logger.Error("failed to check existing user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "имя пользователя уже занято"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("failed to hash password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	user := &domain.User{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         role,
		IsActive:     true,
	}
	if err := h.userRepo.Create(r.Context(), user); err != nil {
		h.logger.Error("failed to create user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusCreated, newUserDTO(user))
}

// handleUpdateUser изменяет данные пользователя (только admin).
func (h *Handler) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор пользователя"})
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get user for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "пользователь не найден"})
		return
	}

	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректное тело запроса"})
		return
	}

	if req.Username != nil {
		name := strings.TrimSpace(*req.Username)
		if len(name) < 3 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "имя пользователя должно содержать не менее 3 символов"})
			return
		}
		if name != user.Username {
			existing, err := h.userRepo.GetByUsername(r.Context(), name)
			if err != nil {
				h.logger.Error("failed to check existing user", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
				return
			}
			if existing != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "имя пользователя уже занято"})
				return
			}
			user.Username = name
		}
	}

	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		if len(*req.Password) < 8 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "пароль должен содержать не менее 8 символов"})
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			h.logger.Error("failed to hash password", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
			return
		}
		user.PasswordHash = hash
	}

	if req.Role != nil {
		newRole := domain.UserRole(*req.Role)
		if !newRole.Valid() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректная роль"})
			return
		}
		if newRole != user.Role && user.Role == domain.RoleAdmin {
			count, err := h.userRepo.CountByRole(r.Context(), domain.RoleAdmin)
			if err != nil {
				h.logger.Error("failed to count admins", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
				return
			}
			if count <= 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "нельзя снять роль администратора с последнего администратора",
				})
				return
			}
		}
		user.Role = newRole
	}

	if req.IsActive != nil {
		if user.IsActive && !*req.IsActive && user.Role == domain.RoleAdmin {
			count, err := h.userRepo.CountByRole(r.Context(), domain.RoleAdmin)
			if err != nil {
				h.logger.Error("failed to count admins", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
				return
			}
			if count <= 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "нельзя отключить последнего администратора",
				})
				return
			}
		}
		user.IsActive = *req.IsActive
	}

	if err := h.userRepo.Update(r.Context(), user); err != nil {
		h.logger.Error("failed to update user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	writeJSON(w, http.StatusOK, newUserDTO(user))
}

// handleDeleteUser удаляет пользователя (только admin).
// Пользователей с ролью admin удалять запрещено.
func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "некорректный идентификатор пользователя"})
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get user for delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "пользователь не найден"})
		return
	}

	if user.Role == domain.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "администратора нельзя удалить — только изменить",
		})
		return
	}

	if err := h.userRepo.Delete(r.Context(), id); err != nil {
		h.logger.Error("failed to delete user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}