-- +goose Up

ALTER TABLE recordings
    ADD CONSTRAINT recordings_storage_path_unique UNIQUE (storage_path);

-- +goose Down

ALTER TABLE recordings
    DROP CONSTRAINT recordings_storage_path_unique;