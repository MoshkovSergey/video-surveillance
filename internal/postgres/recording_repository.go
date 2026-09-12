package postgres

import (
	"context"
	"encoding/json"
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

// InsertKept сохраняет метаданные вырезанного клипа (постоянное хранение).
func (r *RecordingRepository) InsertKept(ctx context.Context, rec *domain.Recording) error {
	query := `
		INSERT INTO recordings (camera_id, started_at, ended_at, storage_path, size_bytes, kept)
		VALUES ($1, $2, $3, $4, $5, true)
		ON CONFLICT (storage_path) DO UPDATE
		SET size_bytes = EXCLUDED.size_bytes,
		    ended_at   = EXCLUDED.ended_at
	`
	_, err := r.pool.Exec(ctx, query,
		rec.CameraID, rec.StartedAt, rec.EndedAt, rec.StoragePath, rec.SizeBytes,
	)
	if err != nil {
		return fmt.Errorf("insert kept recording: %w", err)
	}
	return nil
}

// List возвращает сегменты архива с фильтрами, новые первыми.
func (r *RecordingRepository) List(ctx context.Context, cameraID *uuid.UUID, from, to *time.Time) ([]domain.Recording, error) {
	query := `
		SELECT id, camera_id, started_at, ended_at, storage_path, size_bytes, kept, created_at
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
			&rec.StoragePath, &rec.SizeBytes, &rec.Kept, &rec.CreatedAt,
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

// GetByID возвращает сегмент по идентификатору.
func (r *RecordingRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Recording, error) {
	query := `
		SELECT id, camera_id, started_at, ended_at, storage_path, size_bytes, kept, created_at
		FROM recordings
		WHERE id = $1
	`
	var rec domain.Recording
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&rec.ID, &rec.CameraID, &rec.StartedAt, &rec.EndedAt,
		&rec.StoragePath, &rec.SizeBytes, &rec.Kept, &rec.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query recording: %w", err)
	}
	return &rec, nil
}

// ListOverlappingClosed возвращает закрытые сегменты, пересекающие окно [from, to].
func (r *RecordingRepository) ListOverlappingClosed(ctx context.Context, cameraID uuid.UUID, from, to time.Time) ([]domain.Recording, error) {
	query := `
		SELECT id, camera_id, started_at, ended_at, storage_path, size_bytes, kept, created_at
		FROM recordings
		WHERE camera_id = $1
		  AND ended_at IS NOT NULL
		  AND started_at <= $3
		  AND ended_at >= $2
		ORDER BY started_at
	`
	rows, err := r.pool.Query(ctx, query, cameraID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query overlapping segments: %w", err)
	}
	defer rows.Close()

	var recordings []domain.Recording
	for rows.Next() {
		var rec domain.Recording
		if err := rows.Scan(
			&rec.ID, &rec.CameraID, &rec.StartedAt, &rec.EndedAt,
			&rec.StoragePath, &rec.SizeBytes, &rec.Kept, &rec.CreatedAt,
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

// ListSourceSegments возвращает закрытые буферные сегменты (kept = false),
// пересекающие окно [from, to]: только они могут быть источником кадрирования.
func (r *RecordingRepository) ListSourceSegments(ctx context.Context, cameraID uuid.UUID, from, to time.Time) ([]domain.Recording, error) {
	query := `
		SELECT id, camera_id, started_at, ended_at, storage_path, size_bytes, kept, created_at
		FROM recordings
		WHERE camera_id = $1
		  AND kept = false
		  AND ended_at IS NOT NULL
		  AND started_at <= $3
		  AND ended_at >= $2
		ORDER BY started_at
	`
	rows, err := r.pool.Query(ctx, query, cameraID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query source segments: %w", err)
	}
	defer rows.Close()

	var recordings []domain.Recording
	for rows.Next() {
		var rec domain.Recording
		if err := rows.Scan(
			&rec.ID, &rec.CameraID, &rec.StartedAt, &rec.EndedAt,
			&rec.StoragePath, &rec.SizeBytes, &rec.Kept, &rec.CreatedAt,
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

// HasOpenOverlapping: ждём только если триггер попадает в ТЕКУЩИЙ
// (самый новый) незакрытый буферный сегмент. Старые open-строки
// (например после смены формата пути) игнорируются.
func (r *RecordingRepository) HasOpenOverlapping(ctx context.Context, cameraID uuid.UUID, from, to time.Time) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM recordings r
			WHERE r.camera_id = $1
			  AND r.ended_at IS NULL
			  AND r.kept = false
			  AND r.started_at <= $3
			  AND r.started_at >= ($2::timestamptz - interval '6 hours')
			  AND r.started_at = (
				SELECT MAX(r2.started_at)
				FROM recordings r2
				WHERE r2.camera_id = $1
				  AND r2.ended_at IS NULL
				  AND r2.kept = false
			  )
		)
	`
	var exists bool
	if err := r.pool.QueryRow(ctx, query, cameraID, from, to).Scan(&exists); err != nil {
		return false, fmt.Errorf("check open segment: %w", err)
	}
	return exists, nil
}

// DeleteStaleOpenSegments удаляет «зависшие» open-строки, которые не являются
// самым новым открытым сегментом камеры (дубликаты путей, смена abs/rel).
func (r *RecordingRepository) DeleteStaleOpenSegments(ctx context.Context, cameraID uuid.UUID) (int64, error) {
	query := `
		DELETE FROM recordings r
		WHERE r.camera_id = $1
		  AND r.ended_at IS NULL
		  AND r.kept = false
		  AND r.started_at < (
			SELECT MAX(r2.started_at)
			FROM recordings r2
			WHERE r2.camera_id = $1
			  AND r2.ended_at IS NULL
			  AND r2.kept = false
		  )
	`
	tag, err := r.pool.Exec(ctx, query, cameraID)
	if err != nil {
		return 0, fmt.Errorf("delete stale open segments: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteOrphanPaths удаляет метаданные сегментов, которых нет в актуальном
// списке путей (и abs, и устаревшие relative).
func (r *RecordingRepository) DeleteOrphanPaths(ctx context.Context, cameraID uuid.UUID, keep []string) (int64, error) {
	query := `
		DELETE FROM recordings
		WHERE camera_id = $1
		  AND kept = false
		  AND NOT (storage_path = ANY($2))
	`
	tag, err := r.pool.Exec(ctx, query, cameraID, keep)
	if err != nil {
		return 0, fmt.Errorf("delete orphan paths: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ListMotionBuffer возвращает завершённые сегменты буфера режима «по движению»,
// не помеченные к хранению, новые первыми.
func (r *RecordingRepository) ListMotionBuffer(ctx context.Context, cameraID uuid.UUID) ([]domain.Recording, error) {
	query := `
		SELECT id, camera_id, started_at, ended_at, storage_path, size_bytes, kept, created_at
		FROM recordings
		WHERE camera_id = $1
		  AND kept = false
		  AND ended_at IS NOT NULL
		ORDER BY started_at DESC
	`
	rows, err := r.pool.Query(ctx, query, cameraID)
	if err != nil {
		return nil, fmt.Errorf("query motion buffer: %w", err)
	}
	defer rows.Close()

	var recordings []domain.Recording
	for rows.Next() {
		var rec domain.Recording
		if err := rows.Scan(
			&rec.ID, &rec.CameraID, &rec.StartedAt, &rec.EndedAt,
			&rec.StoragePath, &rec.SizeBytes, &rec.Kept, &rec.CreatedAt,
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

// DeleteByID удаляет метаданные сегмента по идентификатору.
func (r *RecordingRepository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM recordings WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete recording: %w", err)
	}
	return nil
}

// Delete удаляет метаданные сегмента по идентификатору (алиас).
func (r *RecordingRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.DeleteByID(ctx, id)
}

// PruneMissing удаляет метаданные сегментов, файлы которых отсутствуют на диске.
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

// ensureJSON сохраняет зависимость от encoding/json для будущих расширений.
var _ = json.Marshal
