-- +goose Up

CREATE TABLE floor_plans (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    image_path TEXT NOT NULL,
    image_mime TEXT NOT NULL DEFAULT 'image/png',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plan_objects (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id   UUID NOT NULL REFERENCES floor_plans (id) ON DELETE CASCADE,
    kind      TEXT NOT NULL CHECK (kind IN ('camera', 'exit', 'zone')),
    camera_id UUID REFERENCES cameras (id) ON DELETE CASCADE,
    label     TEXT NOT NULL DEFAULT '',
    x         REAL NOT NULL DEFAULT 0,
    y         REAL NOT NULL DEFAULT 0,
    CONSTRAINT plan_objects_camera_required
        CHECK (kind <> 'camera' OR camera_id IS NOT NULL)
);

CREATE INDEX idx_plan_objects_plan ON plan_objects (plan_id);

-- +goose Down

DROP TABLE plan_objects;
DROP TABLE floor_plans;