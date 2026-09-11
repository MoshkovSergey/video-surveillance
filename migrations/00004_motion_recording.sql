-- +goose Up

ALTER TABLE cameras
    ADD COLUMN recording_mode TEXT NOT NULL DEFAULT 'continuous';

ALTER TABLE cameras
    ADD CONSTRAINT cameras_recording_mode_check
    CHECK (recording_mode IN ('continuous', 'motion'));

ALTER TABLE cameras
    ADD COLUMN motion_detection BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE recordings
    ADD COLUMN kept BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX idx_recordings_kept
    ON recordings (camera_id, kept, started_at);

-- +goose Down

DROP INDEX idx_recordings_kept;

ALTER TABLE recordings DROP COLUMN kept;

ALTER TABLE cameras DROP CONSTRAINT cameras_recording_mode_check;
ALTER TABLE cameras DROP COLUMN motion_detection;
ALTER TABLE cameras DROP COLUMN recording_mode;