package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingsRepository реализует хранение настроек в таблице settings.
type SettingsRepository struct {
	pool *pgxpool.Pool
}

// NewSettingsRepository создает экземпляр репозитория.
func NewSettingsRepository(pool *pgxpool.Pool) *SettingsRepository {
	return &SettingsRepository{pool: pool}
}

// All возвращает все настройки в виде карты ключ-значение.
func (r *SettingsRepository) All(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("query settings: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		result[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}
	return result, nil
}

// Get возвращает значение настройки. Если ключа нет — false.
func (r *SettingsRepository) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := r.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query setting: %w", err)
	}
	return value, true, nil
}

// Set сохраняет значение настройки (upsert).
func (r *SettingsRepository) Set(ctx context.Context, key, value string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	if err != nil {
		return fmt.Errorf("upsert setting: %w", err)
	}
	return nil
}