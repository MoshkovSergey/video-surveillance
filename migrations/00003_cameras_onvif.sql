-- +goose Up

ALTER TABLE cameras
    ADD COLUMN source_type TEXT NOT NULL DEFAULT 'rtsp';

ALTER TABLE cameras
    ADD CONSTRAINT cameras_source_type_check
    CHECK (source_type IN ('rtsp', 'onvif'));

ALTER TABLE cameras ADD COLUMN onvif_host TEXT;
ALTER TABLE cameras ADD COLUMN onvif_port INTEGER;
ALTER TABLE cameras ADD COLUMN onvif_username TEXT;
ALTER TABLE cameras ADD COLUMN onvif_password TEXT;
ALTER TABLE cameras ADD COLUMN onvif_profile TEXT;

-- +goose Down

ALTER TABLE cameras DROP CONSTRAINT cameras_source_type_check;
ALTER TABLE cameras DROP COLUMN source_type;
ALTER TABLE cameras DROP COLUMN onvif_host;
ALTER TABLE cameras DROP COLUMN onvif_port;
ALTER TABLE cameras DROP COLUMN onvif_username;
ALTER TABLE cameras DROP COLUMN onvif_password;
ALTER TABLE cameras DROP COLUMN onvif_profile;