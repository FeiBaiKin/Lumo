-- +goose Up
-- 主题系统（agent.md §4）。本模块的迁移编号独立于核心，从 1 起。

-- 主题设置值：每个主题的每个分组一行。
-- 与站点设置分表，是因为两者的生命周期不同——卸载主题时要能连同它的设置一并清掉，
-- 而站点设置是核心资产，混在一张表里迟早会误删。
CREATE TABLE theme_settings (
    theme       text        NOT NULL,
    group_name  text        NOT NULL,
    values      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    updated_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (theme, group_name),
    CONSTRAINT theme_settings_theme_format CHECK (theme ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    CONSTRAINT theme_settings_group_format CHECK (group_name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    CONSTRAINT theme_settings_values_is_object CHECK (jsonb_typeof(values) = 'object')
);

-- 当前启用的主题。单行表：用 CHECK (id = 1) 保证它只能有一行，
-- 比在应用层约定「只读第一行」可靠——后者在并发写入下会出现两行状态。
CREATE TABLE theme_state (
    id          integer     PRIMARY KEY DEFAULT 1,
    active      text        NOT NULL DEFAULT '',
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT theme_state_single_row CHECK (id = 1)
);

-- +goose Down
DROP TABLE theme_state;
DROP TABLE theme_settings;
