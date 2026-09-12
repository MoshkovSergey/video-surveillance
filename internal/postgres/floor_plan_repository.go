package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"gitverse.ru/cataclysm78/video-surveillance/internal/domain"
)

// FloorPlanRepository реализует работу с планами объекта и объектами на них.
type FloorPlanRepository struct {
	pool *pgxpool.Pool
}

// NewFloorPlanRepository создает экземпляр репозитория.
func NewFloorPlanRepository(pool *pgxpool.Pool) *FloorPlanRepository {
	return &FloorPlanRepository{pool: pool}
}

// Create сохраняет план.
func (r *FloorPlanRepository) Create(ctx context.Context, plan *domain.FloorPlan) error {
	if plan.ID == uuid.Nil {
		plan.ID = uuid.New()
	}
	plan.CreatedAt = time.Now()
	plan.UpdatedAt = plan.CreatedAt

	query := `
		INSERT INTO floor_plans (id, name, image_path, image_mime, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.pool.Exec(ctx, query,
		plan.ID, plan.Name, plan.ImagePath, plan.ImageMime, plan.CreatedAt, plan.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert floor plan: %w", err)
	}
	return nil
}

// Get возвращает план по идентификатору.
func (r *FloorPlanRepository) Get(ctx context.Context, id uuid.UUID) (*domain.FloorPlan, error) {
	query := `
		SELECT id, name, image_path, image_mime, created_at, updated_at
		FROM floor_plans
		WHERE id = $1
	`
	var plan domain.FloorPlan
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&plan.ID, &plan.Name, &plan.ImagePath, &plan.ImageMime, &plan.CreatedAt, &plan.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("query floor plan: %w", err)
	}
	return &plan, nil
}

// List возвращает все планы, новые первыми.
func (r *FloorPlanRepository) List(ctx context.Context) ([]domain.FloorPlan, error) {
	query := `
		SELECT id, name, image_path, image_mime, created_at, updated_at
		FROM floor_plans
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query floor plans: %w", err)
	}
	defer rows.Close()

	var plans []domain.FloorPlan
	for rows.Next() {
		var plan domain.FloorPlan
		if err := rows.Scan(
			&plan.ID, &plan.Name, &plan.ImagePath, &plan.ImageMime, &plan.CreatedAt, &plan.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan floor plan: %w", err)
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate floor plans: %w", err)
	}
	return plans, nil
}

// Rename меняет наименование плана.
func (r *FloorPlanRepository) Rename(ctx context.Context, id uuid.UUID, name string) error {
	query := `UPDATE floor_plans SET name = $2, updated_at = now() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id, name)
	if err != nil {
		return fmt.Errorf("rename floor plan: %w", err)
	}
	return nil
}

// Delete удаляет план (объекты удаляются каскадно).
func (r *FloorPlanRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM floor_plans WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete floor plan: %w", err)
	}
	return nil
}

// ListObjects возвращает объекты плана.
func (r *FloorPlanRepository) ListObjects(ctx context.Context, planID uuid.UUID) ([]domain.PlanObject, error) {
	query := `
		SELECT id, plan_id, kind, camera_id, label, x, y
		FROM plan_objects
		WHERE plan_id = $1
		ORDER BY kind, label
	`
	rows, err := r.pool.Query(ctx, query, planID)
	if err != nil {
		return nil, fmt.Errorf("query plan objects: %w", err)
	}
	defer rows.Close()

	var objects []domain.PlanObject
	for rows.Next() {
		var obj domain.PlanObject
		if err := rows.Scan(&obj.ID, &obj.PlanID, &obj.Kind, &obj.CameraID, &obj.Label, &obj.X, &obj.Y); err != nil {
			return nil, fmt.Errorf("scan plan object: %w", err)
		}
		objects = append(objects, obj)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan objects: %w", err)
	}
	return objects, nil
}

// ReplaceObjects атомарно заменяет набор объектов плана.
func (r *FloorPlanRepository) ReplaceObjects(ctx context.Context, planID uuid.UUID, objects []domain.PlanObject) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM plan_objects WHERE plan_id = $1`, planID); err != nil {
		return fmt.Errorf("clear plan objects: %w", err)
	}

	for i := range objects {
		obj := &objects[i]
		if obj.ID == uuid.Nil {
			obj.ID = uuid.New()
		}
		obj.PlanID = planID

		_, err := tx.Exec(ctx, `
			INSERT INTO plan_objects (id, plan_id, kind, camera_id, label, x, y)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, obj.ID, obj.PlanID, obj.Kind, obj.CameraID, obj.Label, obj.X, obj.Y)
		if err != nil {
			return fmt.Errorf("insert plan object: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}