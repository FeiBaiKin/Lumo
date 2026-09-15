-- +goose Up
-- 阶段 1 只建立 Extension 通用表。
-- 强类型核心表（文章、用户等）随阶段 2、3 的功能一并加入各自的迁移文件。

CREATE TABLE extensions (
    id          bigserial   PRIMARY KEY,
    api_group   text        NOT NULL,
    version     text        NOT NULL,
    kind        text        NOT NULL,
    name        text        NOT NULL,
    spec        jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- name 为 DNS-1123：小写字母数字与连字符，不以连字符开头或结尾
    CONSTRAINT extensions_name_dns1123 CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    -- 同一 group/version/kind 下 name 唯一
    CONSTRAINT extensions_unique UNIQUE (api_group, version, kind, name)
);

-- spec 的 JSONB 查询走 GIN
CREATE INDEX extensions_spec_gin ON extensions USING GIN (spec);

-- 按 kind 列表查询是最常见的访问模式
CREATE INDEX extensions_kind_idx ON extensions (api_group, version, kind);

-- +goose Down
DROP TABLE extensions;
