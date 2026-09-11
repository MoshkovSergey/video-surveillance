package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// RecordingRepository реализует работу с таблицей recordings.
type RecordingRepository struct {
	pool *pgxpool.Pool
}

// NewRecordingRepository создает экземпляр репозитория.
func NewRecordingRepository(pool *pgxpool.Pool) *RecordingRepository {
	return &RecordingRepository{pool: pool}
}

// Upsert сохраняет или обновляет метаданные сегмента записи.
func (r *RecordingRepository) Upsert(ctx context.Context, rec *domain.Recording) error {
	query := `
		INSERT INTO recordings (camera_id, started_at, ended_at, storage_path, size_bytes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (storage_path) DO UPDATE
		SET size_bytes = EXCLUDED.size_bytes,
		    ended_at   = EXCLUDED.ended_at
	`
	_, err := r.pool.Exec(ctx, query,
		rec.CameraID, rec.StartedAt, rec.EndedAt, rec.StoragePath, rec.SizeBytes,
	)
	if err != nil {
		return fmt.Errorf("upsert recording: %w", err)
	}
	return nil
}

// List возвращает сегменты архива с фильтрами по камере и времени.
func (r *RecordingRepository) List(ctx context.Context, cameraID *uuid.UUID, from, to *time.Time) ([]domain.Recording, error) {
	query := `
		SELECT id, camera_id, started_at, ended_at, storage_path, size_bytes, created_at
		FROM recordings
		WHERE ($1::uuid IS NULL OR camera_id = $1)
		  AND ($2::timestamptz IS NULL OR started_at >= $2)
		  AND ($3::timestamptz IS NULL OR started_at <= $3)
		ORDER BY started_at DESC
		LIMIT 500
	`
	rows, err := r.pool.Query(ctx, query, cameraID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query recordings: %w", err)
	}
	defer rows.Close()

	var recordings []domain.Recording
	for rows.Next() {
		var rec domain.Recording
		if err := rows.Scan(
			&rec.ID, &rec.CameraID, &rec.StartedAt, &rec.EndedAt,
			&rec.StoragePath, &rec.SizeBytes, &rec.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recording: %w", err)
		}
		recordings = append(recordings, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recordings: %w", err)
	}
	return recordings, nil
}

// GetByID возвращает сегмент по идентификатору. Если не найден — nil, nil.
func (r *RecordingRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Recording, error) {
	query := `
		SELECT id, camera_id, started_at, ended_at, storage_path, size_bytes, created_at
		FROM recordings
		WHERE id = $1
	`
	var rec domain.Recording
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&rec.ID, &rec.CameraID, &rec.StartedAt, &rec.EndedAt,
		&rec.StoragePath, &rec.SizeBytes, &rec.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query recording: %w", err)
	}
	return &rec, nil
}

// Delete удаляет метаданные сегмента по идентификатору.
func (r *RecordingRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM recordings WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete recording: %w", err)
	}
	return nil
}

// PruneMissing удаляет метаданные сегментов, файлы которых отсутствуют на диске.
// prefix ограничивает область действия каталогу хранилища,
// existing — список путей файлов, которые реально существуют.
func (r *RecordingRepository) PruneMissing(ctx context.Context, existing []string, prefix string) (int64, error) {
	query := `
		DELETE FROM recordings
		WHERE storage_path LIKE $1
		  AND NOT (storage_path = ANY($2))
	`
	tag, err := r.pool.Exec(ctx, query, prefix, existing)
	if err != nil {
		return 0, fmt.Errorf("prune recordings: %w", err)
	}
	return tag.RowsAffected(), nil
}