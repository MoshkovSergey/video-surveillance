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
type EventRepository struct {
	pool *pgxpool.Pool
}

// NewEventRepository создает экземпляр репозитория.
func NewEventRepository(pool *pgxpool.Pool) *EventRepository {
	return &EventRepository{pool: pool}
}

// Create сохраняет событие.
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
		INSERT INTO events (id, camera_id, type, severity, occurred_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = r.pool.Exec(ctx, query,
		ev.ID, ev.CameraID, ev.Type, ev.Severity, ev.OccurredAt, payload,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// List возвращает события с фильтрами, новые первыми.
// Пустые строковые фильтры нормализуются в NULL:
// в SQL пустая строка не равна NULL и иначе фильтр отбросил бы все строки.
// Колонка type имеет enum-тип event_type, поэтому сравнение через type::text.
func (r *EventRepository) List(
	ctx context.Context,
	cameraID *uuid.UUID,
	eventType string,
	from, to *time.Time,
	limit int,
) ([]domain.Event, error) {
	var typeParam any
	if eventType != "" {
		typeParam = eventType
	}

	query := `
		SELECT id, camera_id, type, severity, occurred_at, payload, created_at
		FROM events
		WHERE ($1::uuid IS NULL OR camera_id = $1)
		  AND ($2::text IS NULL OR type::text = $2)
		  AND ($3::timestamptz IS NULL OR occurred_at >= $3)
		  AND ($4::timestamptz IS NULL OR occurred_at <= $4)
		ORDER BY occurred_at DESC
		LIMIT $5
	`
	rows, err := r.pool.Query(ctx, query, cameraID, typeParam, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []domain.Event
	for rows.Next() {
		var ev domain.Event
		var payloadBytes []byte

		if err := rows.Scan(
			&ev.ID, &ev.CameraID, &ev.Type, &ev.Severity,
			&ev.OccurredAt, &payloadBytes, &ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		ev.Payload = map[string]any{}
		if payloadBytes != nil {
			if err := json.Unmarshal(payloadBytes, &ev.Payload); err != nil {
				return nil, fmt.Errorf("unmarshal event payload: %w", err)
			}
		}

		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return events, nil
}

// ListSince возвращает события с occurred_at >= since, старые первыми, с лимитом.
func (r *EventRepository) ListSince(ctx context.Context, since time.Time, limit int) ([]domain.Event, error) {
	query := `
		SELECT id, camera_id, type, severity, payload, occurred_at
		FROM events
		WHERE occurred_at >= $1
		ORDER BY occurred_at
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
		var cameraID *uuid.UUID
		var payloadBytes []byte
		if err := rows.Scan(&ev.ID, &cameraID, &ev.Type, &ev.Severity, &payloadBytes, &ev.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.CameraID = cameraID
		if payloadBytes != nil {
			if err := json.Unmarshal(payloadBytes, &ev.Payload); err != nil {
				return nil, fmt.Errorf("unmarshal event payload: %w", err)
			}
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
		SELECT DISTINCT ON (camera_id) camera_id, type
		FROM events
		WHERE type IN ('camera_online', 'camera_offline')
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
		var typ domain.EventType
		if err := rows.Scan(&cameraID, &typ); err != nil {
			return nil, fmt.Errorf("scan camera state: %w", err)
		}
		states[cameraID] = typ == domain.EventCameraOnline
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate camera states: %w", err)
	}
	return states, nil
}