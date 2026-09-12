package domain

import (
	"time"

	"github.com/google/uuid"
)

// PlanObjectKind — тип объекта на плане объекта.
type PlanObjectKind string

const (
	PlanObjectCamera PlanObjectKind = "camera"
	PlanObjectExit   PlanObjectKind = "exit"
	PlanObjectZone   PlanObjectKind = "zone"
)

// FloorPlan — схема этажа/объекта с изображением.
type FloorPlan struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	ImagePath string    `json:"-"`
	ImageMime string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PlanObject — объект, размещённый на плане (камера, выход, зона).
// Координаты x, y — в процентах от ширины и высоты изображения (0..100).
type PlanObject struct {
	ID       uuid.UUID      `json:"id"`
	PlanID   uuid.UUID      `json:"plan_id"`
	Kind     PlanObjectKind `json:"kind"`
	CameraID *uuid.UUID     `json:"camera_id"`
	Label    string         `json:"label"`
	X        float64        `json:"x"`
	Y        float64        `json:"y"`
}