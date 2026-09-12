package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// EventRepository реализует работу с таблицей events.
// Колонка type имеет ENUM-тип event_type, поэтому во всех запросах
// используется приведение type::text.
type EventRepository struct {
	pool *pgxpool.Pool
}

// NewEventRepository создает экземпляр репозитория.
func NewEventRepository(pool *pgxpool.Pool) *EventRepository {
	return &EventRepository{pool: pool}
}

// Create сохраняет событие журнала.
func (r *EventRepository) Create(ctx context.Context, ev *domain.Event) error {
	if ev.ID == uuid.Nil {
		ev.ID = uuid.New()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}

	payload, err := json.Marshal(ev.Payload)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}

	query := `
		INSERT INTO events (id, camera_id, type, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = r.pool.Exec(ctx, query,
		ev.ID, ev.CameraID, string(ev.Type), payload, ev.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// List возвращает события с фильтрами и пагинацией, новые первыми.
func (r *EventRepository) List(
	ctx context.Context,
	cameraID *uuid.UUID,
	typ *domain.EventType,
	from, to *time.Time,
	limit, offset int,
) ([]domain.Event, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var typeStr *string
	if typ != nil {
		s := string(*typ)
		typeStr = &s
	}

	query := `
		SELECT id, camera_id, type::text, payload, occurred_at
		FROM events
		WHERE ($1::uuid IS NULL OR camera_id = $1)
		  AND ($2::text IS NULL OR type::text = $2)
		  AND ($3::timestamptz IS NULL OR occurred_at >= $3)
		  AND ($4::timestamptz IS NULL OR occurred_at <= $4)
		ORDER BY occurred_at DESC
		LIMIT $5 OFFSET $6
	`
	rows, err := r.pool.Query(ctx, query, cameraID, typeStr, from, to, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var ev domain.Event
		var payload []byte
		var typeText string
		if err := rows.Scan(&ev.ID, &ev.CameraID, &typeText, &payload, &ev.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.Type = domain.EventType(typeText)
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &ev.Payload)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}

// ListSince возвращает события строго позже since, старые первыми
// (для отправителя уведомлений).
func (r *EventRepository) ListSince(ctx context.Context, since time.Time, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, camera_id, type::text, payload, occurred_at
		FROM events
		WHERE occurred_at > $1
		ORDER BY occurred_at ASC
		LIMIT $2
	`
	rows, err := r.pool.Query(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("query events since: %w", err)
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var ev domain.Event
		var payload []byte
		var typeText string
		if err := rows.Scan(&ev.ID, &ev.CameraID, &typeText, &payload, &ev.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.Type = domain.EventType(typeText)
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &ev.Payload)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}

// LatestCameraStates возвращает актуальное состояние камер
// по последним событиям camera_online / camera_offline.
func (r *EventRepository) LatestCameraStates(ctx context.Context) (map[uuid.UUID]bool, error) {
	query := `
		SELECT DISTINCT ON (camera_id) camera_id, type::text
		FROM events
		WHERE type::text IN ('camera_online', 'camera_offline')
		  AND camera_id IS NOT NULL
		ORDER BY camera_id, occurred_at DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query camera states: %w", err)
	}
	defer rows.Close()

	states := make(map[uuid.UUID]bool)
	for rows.Next() {
		var cameraID uuid.UUID
		var typeText string
		if err := rows.Scan(&cameraID, &typeText); err != nil {
			return nil, fmt.Errorf("scan camera state: %w", err)
		}
		states[cameraID] = typeText == string(domain.EventCameraOnline)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate camera states: %w", err)
	}
	return states, nil
}