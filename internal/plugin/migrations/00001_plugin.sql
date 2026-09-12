-- +goose Up
-- 插件生命周期（agent.md §14.3）。
--
-- 插件状态是核心资产，与主题的 theme_state 同级，故用强类型表而不是 extensions：
-- 后者经 Extension 平面开放通用 CRUD，插件记录被随手改写会让目录与库对不上。

CREATE TABLE plugins (
    name            text        PRIMARY KEY,
    version         text        NOT NULL,
    display_name    text        NOT NULL,
    description     text        NOT NULL DEFAULT '',
    author          text        NOT NULL DEFAULT '',
    enabled         boolean     NOT NULL DEFAULT false,
    -- 清单原文：升级时用来比对，也省得为每个字段都加一列。
    manifest        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    installed_at    timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    -- 与目录名同形（agent.md §6.1 的 DNS-1123）
    CONSTRAINT plugins_name_dns1123 CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')
);

-- 插件的设置值。与 theme_settings 同构：随插件走，卸载时一并清掉。
-- 故用外键级联而不是在代码里记得删——「记得删」这件事迟早会有人忘。
CREATE TABLE plugin_settings (
    plugin      text        NOT NULL REFERENCES plugins(name) ON DELETE CASCADE,
    group_name  text        NOT NULL,
    values      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    updated_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (plugin, group_name)
);

-- +goose Down
DROP TABLE plugin_settings;
DROP TABLE plugins;
