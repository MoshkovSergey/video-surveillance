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

const cameraColumns = `id, name, rtsp_uri, location, fire_zone_id, status, config,
	created_at, updated_at, source_type, onvif_host, onvif_port, onvif_username,
	onvif_password, onvif_profile, recording_mode, motion_detection`

// Create сохраняет новую камеру в базе данных.
func (r *CameraRepository) Create(ctx context.Context, cam *domain.Camera) error {
	query := `
		INSERT INTO cameras (
			id, name, rtsp_uri, location, fire_zone_id, status, config,
			created_at, updated_at,
			source_type, onvif_host, onvif_port, onvif_username, onvif_password, onvif_profile,
			recording_mode, motion_detection
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`

	if cam.ID == uuid.Nil {
		cam.ID = uuid.New()
	}
	now := time.Now()
	cam.CreatedAt = now
	cam.UpdatedAt = now

	configBytes, err := json.Marshal(cam.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	host, port, username, password, profile := onvifColumns(cam)

	_, err = r.pool.Exec(ctx, query,
		cam.ID, cam.Name, cam.RTSPUri, cam.Location, cam.FireZoneID,
		cam.Status, configBytes, cam.CreatedAt, cam.UpdatedAt,
		cam.SourceType, host, port, username, password, profile,
		cam.RecordingMode, cam.MotionDetection,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("camera already exists: %w", err)
		}
		return fmt.Errorf("insert camera: %w", err)
	}
	return nil
}

// List возвращает список всех камер.
func (r *CameraRepository) List(ctx context.Context) ([]domain.Camera, error) {
	query := `SELECT ` + cameraColumns + ` FROM cameras ORDER BY created_at DESC`
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

// GetByID возвращает камеру по идентификатору. Если не найдена — nil, nil.
func (r *CameraRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Camera, error) {
	query := `SELECT ` + cameraColumns + ` FROM cameras WHERE id = $1`
	row := r.pool.QueryRow(ctx, query, id)
	cam, err := scanCamera(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query camera: %w", err)
	}
	return cam, nil
}

// Update обновляет данные камеры.
func (r *CameraRepository) Update(ctx context.Context, cam *domain.Camera) error {
	query := `
		UPDATE cameras
		SET name = $1, rtsp_uri = $2, location = $3, fire_zone_id = $4, status = $5, config = $6,
		    updated_at = $7,
		    source_type = $8, onvif_host = $9, onvif_port = $10, onvif_username = $11,
		    onvif_password = $12, onvif_profile = $13,
		    recording_mode = $14, motion_detection = $15
		WHERE id = $16
	`
	cam.UpdatedAt = time.Now()

	configBytes, err := json.Marshal(cam.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	host, port, username, password, profile := onvifColumns(cam)

	tag, err := r.pool.Exec(ctx, query,
		cam.Name, cam.RTSPUri, cam.Location, cam.FireZoneID,
		cam.Status, configBytes, cam.UpdatedAt,
		cam.SourceType, host, port, username, password, profile,
		cam.RecordingMode, cam.MotionDetection,
		cam.ID,
	)
	if err != nil {
		return fmt.Errorf("update camera: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

// SetStatus обновляет только статус камеры.
func (r *CameraRepository) SetStatus(ctx context.Context, id uuid.UUID, status domain.CameraStatus) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE cameras SET status = $1, updated_at = now() WHERE id = $2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("set camera status: %w", err)
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
		return nil
	}
	return nil
}

// onvifColumns раскладывает параметры ONVIF в nullable-колонки.
func onvifColumns(cam *domain.Camera) (host *string, port *int, username *string, password *string, profile *string) {
	if cam.ONVIF == nil {
		return nil, nil, nil, nil, nil
	}
	host = &cam.ONVIF.Host
	port = &cam.ONVIF.Port
	username = &cam.ONVIF.Username
	password = &cam.ONVIF.Password
	profile = &cam.ONVIF.Profile
	return host, port, username, password, profile
}

// scanCamera универсальная функция чтения строки БД в структуру Camera.
func scanCamera(sc interface{ Scan(dest ...any) error }) (*domain.Camera, error) {
	var cam domain.Camera
	var configBytes []byte
	var sourceType string
	var recordingMode string
	var onvifHost *string
	var onvifPort *int
	var onvifUsername *string
	var onvifPassword *string
	var onvifProfile *string

	err := sc.Scan(
		&cam.ID, &cam.Name, &cam.RTSPUri, &cam.Location, &cam.FireZoneID,
		&cam.Status, &configBytes, &cam.CreatedAt, &cam.UpdatedAt,
		&sourceType, &onvifHost, &onvifPort, &onvifUsername, &onvifPassword, &onvifProfile,
		&recordingMode, &cam.MotionDetection,
	)
	if err != nil {
		return nil, err
	}

	if configBytes != nil {
		if err := json.Unmarshal(configBytes, &cam.Config); err != nil {
			return nil, fmt.Errorf("unmarshal config: %w", err)
		}
	}

	cam.SourceType = domain.CameraSourceType(sourceType)
	if cam.SourceType == "" {
		cam.SourceType = domain.SourceRTSP
	}

	cam.RecordingMode = domain.RecordingMode(recordingMode)
	if cam.RecordingMode == "" {
		cam.RecordingMode = domain.RecordingContinuous
	}

	if onvifHost != nil {
		onvif := &domain.ONVIFParams{Host: *onvifHost}
		if onvifPort != nil {
			onvif.Port = *onvifPort
		}
		if onvifUsername != nil {
			onvif.Username = *onvifUsername
		}
		if onvifPassword != nil {
			onvif.Password = *onvifPassword
		}
		if onvifProfile != nil {
			onvif.Profile = *onvifProfile
		}
		cam.ONVIF = onvif
	}

	return &cam, nil
}