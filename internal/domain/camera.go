package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CameraStatus определяет статус камеры.
type CameraStatus string

const (
	CameraStatusEnabled  CameraStatus = "enabled"
	CameraStatusDisabled CameraStatus = "disabled"
	CameraStatusError    CameraStatus = "error"
)

// Camera представляет камеру видеонаблюдения.
type Camera struct {
	ID         uuid.UUID      `json:"id"`
	Name       string         `json:"name"`
	RTSPUri    string         `json:"rtsp_uri"`
	Location   string         `json:"location,omitempty"`
	FireZoneID *uuid.UUID     `json:"fire_zone_id,omitempty"` // Привязка к зоне пожарной безопасности
	Status     CameraStatus   `json:"status"`
	Config     map[string]any `json:"config"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// Validate проверяет базовую корректность данных камеры.
func (c *Camera) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("camera name is required")
	}
	if c.RTSPUri == "" {
		return fmt.Errorf("camera rtsp uri is required")
	}
	switch c.Status {
	case CameraStatusEnabled, CameraStatusDisabled, CameraStatusError:
		// ok
	case "":
		c.Status = CameraStatusEnabled // Значение по умолчанию
	default:
		return fmt.Errorf("invalid camera status: %s", c.Status)
	}
	return nil
}