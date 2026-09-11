package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// CameraRepository реализует работу с таблицей cameras.
type CameraRepository struct {
	pool *pgxpool.Pool
}

// NewCameraRepository создает экземпляр репозитория.
func NewCameraRepository(pool *pgxpool.Pool) *CameraRepository {
	return &CameraRepository{pool: pool}
}

// Create сохраняет новую камеру в базе данных.
func (r *CameraRepository) Create(ctx context.Context, cam *domain.Camera) error {
	query := `
		INSERT INTO cameras (id, name, rtsp_uri, location, fire_zone_id, status, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	if cam.ID == uuid.Nil {
		cam.ID = uuid.New()
	}
	now := time.Now()
	cam.CreatedAt = now
	cam.UpdatedAt = now

	// Преобразуем map[string]any в JSON bytes для надежной записи в JSONB
	configBytes, err := json.Marshal(cam.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = r.pool.Exec(ctx, query,
		cam.ID, cam.Name, cam.RTSPUri, cam.Location, cam.FireZoneID,
		cam.Status, configBytes, cam.CreatedAt, cam.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return fmt.Errorf("camera already exists: %w", err)
		}
		return fmt.Errorf("insert camera: %w", err)
	}
	return nil
}

// List возвращает список всех камер.
func (r *CameraRepository) List(ctx context.Context) ([]domain.Camera, error) {
	query := `
		SELECT id, name, rtsp_uri, location, fire_zone_id, status, config, created_at, updated_at
		FROM cameras
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query cameras: %w", err)
	}
	defer rows.Close()

	var cameras []domain.Camera
	for rows.Next() {
		cam, err := scanCamera(rows)
		if err != nil {
			return nil, err
		}
		cameras = append(cameras, *cam)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cameras: %w", err)
	}
	return cameras, nil
}

// GetByID возвращает камеру по идентификатору.
// Если камера не найдена, возвращает nil, nil.
func (r *CameraRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Camera, error) {
	query := `
		SELECT id, name, rtsp_uri, location, fire_zone_id, status, config, created_at, updated_at
		FROM cameras
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	cam, err := scanCamera(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("query camera: %w", err)
	}
	return cam, nil
}

// Update обновляет данные камеры.
func (r *CameraRepository) Update(ctx context.Context, cam *domain.Camera) error {
	query := `
		UPDATE cameras
		SET name = $1, rtsp_uri = $2, location = $3, fire_zone_id = $4, status = $5, config = $6, updated_at = $7
		WHERE id = $8
	`
	cam.UpdatedAt = time.Now()

	configBytes, err := json.Marshal(cam.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tag, err := r.pool.Exec(ctx, query,
		cam.Name, cam.RTSPUri, cam.Location, cam.FireZoneID,
		cam.Status, configBytes, cam.UpdatedAt, cam.ID,
	)
	if err != nil {
		return fmt.Errorf("update camera: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil // Not found or no changes
	}
	return nil
}

// Delete удаляет камеру по идентификатору.
func (r *CameraRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM cameras WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete camera: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil // Not found
	}
	return nil
}

// scanCamera универсальная функция для чтения строки из БД в структуру Camera.
func scanCamera(sc interface{ Scan(dest ...any) error }) (*domain.Camera, error) {
	var cam domain.Camera
	var configBytes []byte

	err := sc.Scan(
		&cam.ID, &cam.Name, &cam.RTSPUri, &cam.Location, &cam.FireZoneID,
		&cam.Status, &configBytes, &cam.CreatedAt, &cam.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if configBytes != nil {
		if err := json.Unmarshal(configBytes, &cam.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
	}

	return &cam, nil
}
