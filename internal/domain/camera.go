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

// RecordingMode определяет режим записи камеры.
type RecordingMode string

const (
	// RecordingContinuous — непрерывная запись всех сегментов.
	RecordingContinuous RecordingMode = "continuous"
	// RecordingMotion — в архив сохраняются только эпизоды движения.
	RecordingMotion RecordingMode = "motion"
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
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	RTSPUri         string           `json:"rtsp_uri"`
	Location        string           `json:"location,omitempty"`
	FireZoneID      *uuid.UUID       `json:"fire_zone_id,omitempty"`
	Status          CameraStatus     `json:"status"`
	SourceType      CameraSourceType `json:"source_type"`
	ONVIF           *ONVIFParams     `json:"onvif,omitempty"`
	RecordingMode   RecordingMode    `json:"recording_mode"`
	MotionDetection bool             `json:"motion_detection"`
	Config          map[string]any   `json:"config"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// NormalizeRTSPUri percent-кодирует символы, которые не могут присутствовать
// в RTSP-URL в сыром виде.
func NormalizeRTSPUri(raw string) string {
	return strings.ReplaceAll(strings.TrimSpace(raw), "#", "%23")
}

// Validate проверяет базовую корректность данных камеры.
func (c *Camera) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("укажите название камеры")
	}
	if strings.TrimSpace(c.RTSPUri) == "" {
		return fmt.Errorf("укажите RTSP-адрес камеры")
	}

	u, err := url.Parse(c.RTSPUri)
	if err != nil {
		return fmt.Errorf("некорректный RTSP-адрес: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("некорректный RTSP-адрес: нужны схема и хост")
	}
	if u.Fragment != "" {
		return fmt.Errorf("некорректный RTSP-адрес: символ '#' должен быть закодирован")
	}

	if c.SourceType == "" {
		c.SourceType = SourceRTSP
	}
	if c.SourceType != SourceRTSP && c.SourceType != SourceONVIF {
		return fmt.Errorf("некорректный тип источника: %s", c.SourceType)
	}

	if c.RecordingMode == "" {
		c.RecordingMode = RecordingContinuous
	}
	if c.RecordingMode != RecordingContinuous && c.RecordingMode != RecordingMotion {
		return fmt.Errorf("некорректный режим записи: %s", c.RecordingMode)
	}

	switch c.Status {
	case CameraStatusEnabled, CameraStatusDisabled, CameraStatusError:
		// ok
	case "":
		c.Status = CameraStatusEnabled
	default:
		return fmt.Errorf("некорректный статус камеры: %s", c.Status)
	}
	return nil
}