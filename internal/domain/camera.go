package domain

import (
	"fmt"
	"net/url"
	"strings"
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

// CameraSourceType определяет способ подключения камеры.
type CameraSourceType string

const (
	SourceRTSP  CameraSourceType = "rtsp"
	SourceONVIF CameraSourceType = "onvif"
)

// ONVIFParams — параметры подключения к ONVIF-устройству.
type ONVIFParams struct {
	Host     string `json:"host"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Profile  string `json:"profile,omitempty"`
}

// Camera представляет камеру видеонаблюдения.
type Camera struct {
	ID         uuid.UUID        `json:"id"`
	Name       string           `json:"name"`
	RTSPUri    string           `json:"rtsp_uri"`
	Location   string           `json:"location,omitempty"`
	FireZoneID *uuid.UUID       `json:"fire_zone_id,omitempty"`
	Status     CameraStatus     `json:"status"`
	SourceType CameraSourceType `json:"source_type"`
	ONVIF      *ONVIFParams     `json:"onvif,omitempty"`
	Config     map[string]any   `json:"config"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// NormalizeRTSPUri percent-кодирует символы, которые не могут присутствовать
// в RTSP-URL в сыром виде. Символ '#' начинается фрагмент URL, который
// потоковые серверы отвергают, поэтому он всегда передаётся как %23.
func NormalizeRTSPUri(raw string) string {
	return strings.ReplaceAll(strings.TrimSpace(raw), "#", "%23")
}

// Validate проверяет базовую корректность данных камеры.
func (c *Camera) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("camera name is required")
	}
	if strings.TrimSpace(c.RTSPUri) == "" {
		return fmt.Errorf("camera rtsp uri is required")
	}

	u, err := url.Parse(c.RTSPUri)
	if err != nil {
		return fmt.Errorf("invalid rtsp uri: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid rtsp uri: scheme and host are required")
	}
	if u.Fragment != "" {
		return fmt.Errorf("invalid rtsp uri: unencoded '#' is not allowed")
	}

	if c.SourceType == "" {
		c.SourceType = SourceRTSP
	}
	if c.SourceType != SourceRTSP && c.SourceType != SourceONVIF {
		return fmt.Errorf("invalid source type: %s", c.SourceType)
	}

	switch c.Status {
	case CameraStatusEnabled, CameraStatusDisabled, CameraStatusError:
		// ok
	case "":
		c.Status = CameraStatusEnabled
	default:
		return fmt.Errorf("invalid camera status: %s", c.Status)
	}
	return nil
}