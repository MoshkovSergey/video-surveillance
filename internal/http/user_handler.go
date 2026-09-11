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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if len(req.Username) < 3 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username must be at least 3 characters"})
		return
	}
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
		return
	}

	role := domain.UserRole(req.Role)
	if !role.Valid() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}

	existing, err := h.userRepo.GetByUsername(r.Context(), req.Username)
	if err != nil {
		h.logger.Error("failed to check existing user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("failed to hash password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusCreated, newUserDTO(user))
}

// handleUpdateUser изменяет данные пользователя (только admin).
func (h *Handler) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get user for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Username != nil {
		name := strings.TrimSpace(*req.Username)
		if len(name) < 3 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username must be at least 3 characters"})
			return
		}
		if name != user.Username {
			existing, err := h.userRepo.GetByUsername(r.Context(), name)
			if err != nil {
				h.logger.Error("failed to check existing user", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			if existing != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "username already exists"})
				return
			}
			user.Username = name
		}
	}

	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		if len(*req.Password) < 8 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password must be at least 8 characters"})
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			h.logger.Error("failed to hash password", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		user.PasswordHash = hash
	}

	if req.Role != nil {
		newRole := domain.UserRole(*req.Role)
		if !newRole.Valid() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
			return
		}
		if newRole != user.Role && user.Role == domain.RoleAdmin {
			count, err := h.userRepo.CountByRole(r.Context(), domain.RoleAdmin)
			if err != nil {
				h.logger.Error("failed to count admins", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			if count <= 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "cannot remove admin role from the last administrator",
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
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
				return
			}
			if count <= 1 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "cannot disable the last administrator",
				})
				return
			}
		}
		user.IsActive = *req.IsActive
	}

	if err := h.userRepo.Update(r.Context(), user); err != nil {
		h.logger.Error("failed to update user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, newUserDTO(user))
}

// handleDeleteUser удаляет пользователя (только admin).
// Пользователей с ролью admin удалять запрещено.
func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}

	user, err := h.userRepo.GetByID(r.Context(), id)
	if err != nil {
		h.logger.Error("failed to get user for delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}

	if user.Role == domain.RoleAdmin {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "administrator cannot be deleted, only edited",
		})
		return
	}

	if err := h.userRepo.Delete(r.Context(), id); err != nil {
		h.logger.Error("failed to delete user", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}