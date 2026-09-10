-- +goose Up
-- 分类（树形）与标签（agent.md §8）。本模块的迁移编号独立于核心，从 1 起。

CREATE TABLE categories (
    id          bigserial   PRIMARY KEY,
    -- 父分类。删除父分类时由服务层把子分类挂到祖父分类，SET NULL 仅作兜底
    parent_id   bigint      REFERENCES categories (id) ON DELETE SET NULL,
    name        text        NOT NULL,
    -- slug 保留 Unicode（含中文），不含空白与斜杠；生成规则见 internal/slug
    slug        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    cover_url   text        NOT NULL DEFAULT '',
    -- 同级排序，越小越靠前
    position    integer     NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT categories_name_length CHECK (char_length(name) BETWEEN 1 AND 64),
    CONSTRAINT categories_slug_format CHECK (slug ~ '^[^\s/]{1,128}$'),
    CONSTRAINT categories_no_self_parent CHECK (parent_id IS NULL OR parent_id <> id)
);

-- slug 在全部分类中唯一，它是公开 URL 的一部分
CREATE UNIQUE INDEX categories_slug_key ON categories (slug);
-- 同一父分类下名称大小写不敏感唯一；根分类的 parent_id 为 NULL，用 0 代替参与比较
CREATE UNIQUE INDEX categories_sibling_name_key ON categories (COALESCE(parent_id, 0), lower(name));
-- 按父分类取子分类并排序是最常见的访问模式
CREATE INDEX categories_parent_idx ON categories (parent_id, position);

CREATE TABLE tags (
    id          bigserial   PRIMARY KEY,
    name        text        NOT NULL,
    slug        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    -- 展示用颜色，如 #3b82f6；空串表示使用主题默认
    color       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tags_name_length CHECK (char_length(name) BETWEEN 1 AND 64),
    CONSTRAINT tags_slug_format CHECK (slug ~ '^[^\s/]{1,128}$'),
    CONSTRAINT tags_color_format CHECK (color = '' OR color ~ '^#[0-9a-fA-F]{6}$')
);

CREATE UNIQUE INDEX tags_slug_key ON tags (slug);
-- 标签名大小写不敏感唯一：Go 与 go 应是同一个标签
CREATE UNIQUE INDEX tags_name_key ON tags (lower(name));

-- +goose Down
DROP TABLE tags;
DROP TABLE categories;
