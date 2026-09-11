package domain

import (
	"time"

	"github.com/google/uuid"
)

// EventType определяет тип события системы.
type EventType string

const (
	EventMotion          EventType = "motion"
	EventCameraOnline    EventType = "camera_online"
	EventCameraOffline   EventType = "camera_offline"
	EventFireAlarm       EventType = "fire_alarm"
	EventSmokeDetection  EventType = "smoke_detection"
	EventManualAlarm     EventType = "manual_alarm"
	EventRecordingError  EventType = "recording_error"
)

// EventSeverity определяет важность события.
type EventSeverity string

const (
	SeverityInfo     EventSeverity = "info"
	SeverityWarning  EventSeverity = "warning"
	SeverityCritical EventSeverity = "critical"
)

// Event представляет событие системы видеонаблюдения.
type Event struct {
	ID         uuid.UUID       `json:"id"`
	CameraID   *uuid.UUID      `json:"camera_id,omitempty"`
	Type       EventType       `json:"type"`
	Severity   EventSeverity   `json:"severity"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    map[string]any  `json:"payload"`
	CreatedAt  time.Time       `json:"created_at"`
}