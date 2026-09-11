package domain

import (
	"time"

	"github.com/google/uuid"
)

// Recording представляет сегмент видеоархива.
type Recording struct {
	ID          uuid.UUID  `json:"id"`
	CameraID    uuid.UUID  `json:"camera_id"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	StoragePath string     `json:"storage_path"`
	SizeBytes   int64      `json:"size_bytes"`
	Kept        bool       `json:"kept"`
	CreatedAt   time.Time  `json:"created_at"`
}