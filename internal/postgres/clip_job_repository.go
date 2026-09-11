package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// ClipJobRepository реализует работу с таблицей clip_jobs.
type ClipJobRepository struct {
	pool *pgxpool.Pool
}

// NewClipJobRepository создает экземпляр репозитория.
func NewClipJobRepository(pool *pgxpool.Pool) *ClipJobRepository {
	return &ClipJobRepository{pool: pool}
}

// Create ставит задачу кадрирования в очередь.
func (r *ClipJobRepository) Create(ctx context.Context, job *domain.ClipJob) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.Status == "" {
		job.Status = "pending"
	}
	job.CreatedAt = time.Now()
	job.UpdatedAt = job.CreatedAt

	query := `
		INSERT INTO clip_jobs (id, camera_id, window_start, window_end, status, error, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.pool.Exec(ctx, query,
		job.ID, job.CameraID, job.WindowStart, job.WindowEnd,
		job.Status, job.Error, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert clip job: %w", err)
	}
	return nil
}

// ListPending возвращает задачи в ожидании, старые первыми.
func (r *ClipJobRepository) ListPending(ctx context.Context) ([]domain.ClipJob, error) {
	query := `
		SELECT id, camera_id, window_start, window_end, status, error, created_at, updated_at
		FROM clip_jobs
		WHERE status = 'pending'
		ORDER BY created_at
		LIMIT 20
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query clip jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.ClipJob
	for rows.Next() {
		var job domain.ClipJob
		if err := rows.Scan(
			&job.ID, &job.CameraID, &job.WindowStart, &job.WindowEnd,
			&job.Status, &job.Error, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan clip job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clip jobs: %w", err)
	}
	return jobs, nil
}

// SetStatus переводит задачу в done/failed.
func (r *ClipJobRepository) SetStatus(ctx context.Context, id uuid.UUID, status, errMsg string) error {
	query := `
		UPDATE clip_jobs
		SET status = $2, error = $3, updated_at = now()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id, status, errMsg)
	if err != nil {
		return fmt.Errorf("update clip job: %w", err)
	}
	return nil
}
