-- +goose Up
-- 插件的键值存储：零碎状态、计数器、缓存。值是 JSON，可设过期时间。
-- 不挂外键：站长卸载插件时可以选择保留数据，数据要能比插件记录活得久。
CREATE TABLE plugin_kv (
    plugin      text        NOT NULL,
    key         text        NOT NULL,
    value       jsonb       NOT NULL,
    expires_at  timestamptz,
    updated_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (plugin, key)
);

-- 过期清理只扫有期限的那些
CREATE INDEX plugin_kv_expires_idx ON plugin_kv (expires_at) WHERE expires_at IS NOT NULL;

-- 设置不再随插件记录级联删除，理由同上：卸载时保留数据，设置也在「数据」之列。
-- 删不删改由代码在卸载时显式决定。
ALTER TABLE plugin_settings DROP CONSTRAINT IF EXISTS plugin_settings_plugin_fkey;

-- 卸载了、但站长选择保留数据的插件。后台据此列出「残留数据」，可以单独删掉；
-- 同名插件重装时这一行被移除，数据原样接回去。
CREATE TABLE plugin_retained (
    name          text        PRIMARY KEY,
    display_name  text        NOT NULL,
    version       text        NOT NULL,
    retained_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE plugin_retained;
DELETE FROM plugin_settings WHERE plugin NOT IN (SELECT name FROM plugins);
ALTER TABLE plugin_settings
    ADD CONSTRAINT plugin_settings_plugin_fkey FOREIGN KEY (plugin) REFERENCES plugins(name) ON DELETE CASCADE;
DROP TABLE plugin_kv;
