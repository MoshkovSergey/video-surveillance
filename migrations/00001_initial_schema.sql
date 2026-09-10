-- +goose Up

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE camera_status AS ENUM (
    'enabled',
    'disabled',
    'error'
);

CREATE TYPE event_type AS ENUM (
    'motion',
    'camera_online',
    'camera_offline',
    'fire_alarm',
    'smoke_detection',
    'manual_alarm',
    'recording_error'
);

CREATE TYPE event_severity AS ENUM (
    'info',
    'warning',
    'critical'
);

CREATE TYPE user_role AS ENUM (
    'admin',
    'operator',
    'viewer',
    'auditor'
);

CREATE TABLE fire_zones (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE cameras (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    name TEXT NOT NULL,
    rtsp_uri TEXT NOT NULL,
    location TEXT,

    fire_zone_id UUID REFERENCES fire_zones(id),

    status camera_status NOT NULL DEFAULT 'enabled',
    config JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT cameras_name_not_empty CHECK (length(trim(name)) > 0),
    CONSTRAINT cameras_rtsp_uri_not_empty CHECK (length(trim(rtsp_uri)) > 0)
);

CREATE TABLE recordings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    camera_id UUID NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,

    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,

    storage_path TEXT NOT NULL,
    size_bytes BIGINT,
    duration_seconds NUMERIC,

    codec TEXT,
    width INT,
    height INT,
    fps NUMERIC,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT recordings_storage_path_not_empty CHECK (length(trim(storage_path)) > 0),
    CONSTRAINT recordings_time_range CHECK (ended_at IS NULL OR ended_at >= started_at)
);

CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    camera_id UUID REFERENCES cameras(id) ON DELETE SET NULL,

    type event_type NOT NULL,
    severity event_severity NOT NULL DEFAULT 'info',

    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,

    role user_role NOT NULL DEFAULT 'viewer',
    is_active BOOLEAN NOT NULL DEFAULT true,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT users_username_not_empty CHECK (length(trim(username)) > 0),
    CONSTRAINT users_password_hash_not_empty CHECK (length(trim(password_hash)) > 0)
);

CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID REFERENCES users(id) ON DELETE SET NULL,

    action TEXT NOT NULL,
    entity_type TEXT,
    entity_id UUID,

    payload JSONB NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT audit_logs_action_not_empty CHECK (length(trim(action)) > 0)
);

CREATE INDEX idx_cameras_status ON cameras(status);
CREATE INDEX idx_cameras_fire_zone_id ON cameras(fire_zone_id);

CREATE INDEX idx_recordings_camera_started_at ON recordings(camera_id, started_at DESC);
CREATE INDEX idx_recordings_started_at ON recordings(started_at DESC);

CREATE INDEX idx_events_camera_occurred_at ON events(camera_id, occurred_at DESC);
CREATE INDEX idx_events_type_occurred_at ON events(type, occurred_at DESC);
CREATE INDEX idx_events_severity_occurred_at ON events(severity, occurred_at DESC);

CREATE INDEX idx_audit_logs_user_created_at ON audit_logs(user_id, created_at DESC);
CREATE INDEX idx_audit_logs_entity ON audit_logs(entity_type, entity_id);

COMMENT ON TABLE fire_zones IS 'Зоны пожарной безопасности или технологические зоны объекта';
COMMENT ON TABLE cameras IS 'Каталог камер видеонаблюдения';
COMMENT ON TABLE recordings IS 'Сегменты видеоархива';
COMMENT ON TABLE events IS 'События системы видеонаблюдения и внешних систем';
COMMENT ON TABLE users IS 'Пользователи системы';
COMMENT ON TABLE audit_logs IS 'Журнал действий пользователей и системы';

COMMENT ON COLUMN cameras.fire_zone_id IS 'Привязка камеры к зоне пожарной безопасности';
COMMENT ON COLUMN events.payload IS 'Дополнительные данные события в формате JSON';

-- +goose Down

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS recordings;
DROP TABLE IF EXISTS cameras;
DROP TABLE IF EXISTS fire_zones;

DROP TYPE IF EXISTS user_role;
DROP TYPE IF EXISTS event_severity;
DROP TYPE IF EXISTS event_type;
DROP TYPE IF EXISTS camera_status;
