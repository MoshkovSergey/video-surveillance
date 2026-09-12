-- +goose Up

ALTER TABLE clip_jobs
    ADD COLUMN snapshot_rel TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE clip_jobs
    DROP COLUMN snapshot_rel;