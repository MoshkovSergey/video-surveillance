package domain

import (
	"time"

	"github.com/google/uuid"
)

// ClipJob — задача вырезания точного фрагмента архива по эпизоду движения.
type ClipJob struct {
	ID          uuid.UUID `json:"id"`
	CameraID    uuid.UUID `json:"camera_id"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Status      string    `json:"status"` // pending | done | failed
	Error       string    `json:"error"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
