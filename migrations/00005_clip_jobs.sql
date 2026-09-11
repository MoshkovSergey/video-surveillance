-- +goose Up

CREATE TABLE clip_jobs (
    id           UUID PRIMARY KEY,
    camera_id    UUID NOT NULL REFERENCES cameras (id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    window_end   TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    error        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_clip_jobs_status ON clip_jobs (status);

-- +goose Down

DROP TABLE clip_jobs;