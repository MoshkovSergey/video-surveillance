package domain

import (
	"time"

	"github.com/google/uuid"
)

// UserRole определяет роль пользователя в системе.
type UserRole string

const (
	RoleAdmin    UserRole = "admin"
	RoleOperator UserRole = "operator"
	RoleViewer   UserRole = "viewer"
	RoleAuditor  UserRole = "auditor"
)

// Valid проверяет, что роль существует.
func (r UserRole) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer, RoleAuditor:
		return true
	default:
		return false
	}
}

// User представляет пользователя системы.
type User struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         UserRole  `json:"role"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
