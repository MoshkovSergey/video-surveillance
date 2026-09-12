-- +goose Up

-- Убираем дубли задач на одно окно, оставляя самую позднюю.
DELETE FROM clip_jobs a USING clip_jobs b
WHERE a.created_at < b.created_at
  AND a.camera_id = b.camera_id
  AND a.window_start = b.window_start;

-- Одна задача на окно эпизода.
CREATE UNIQUE INDEX idx_clip_jobs_unique_window
    ON clip_jobs (camera_id, window_start);

-- +goose Down

DROP INDEX idx_clip_jobs_unique_window;