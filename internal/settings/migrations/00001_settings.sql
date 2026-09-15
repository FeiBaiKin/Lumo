-- +goose Up
-- 站点设置：每个分组一行，值为合并默认值后的完整对象。

CREATE TABLE settings (
    name        text        PRIMARY KEY,
    values      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT settings_name_format CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    CONSTRAINT settings_values_is_object CHECK (jsonb_typeof(values) = 'object')
);

-- +goose Down
DROP TABLE settings;
