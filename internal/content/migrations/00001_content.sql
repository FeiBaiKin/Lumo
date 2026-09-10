-- +goose Up
-- 文章与独立页面（agent.md §3.3、§8）。本模块的迁移编号独立于核心，从 1 起。
-- 依赖 taxonomy 模块的 categories / tags 表：modules.go 中 taxonomy 必须先于 content 注册。

CREATE TABLE posts (
    id            bigserial   PRIMARY KEY,
    -- 文章与页面共用一张表（WordPress 模式）：发布、修订、定时、可见性逻辑只写一份
    type          text        NOT NULL,
    title         text        NOT NULL,
    -- slug 在同一类型内唯一；文章与页面走不同的前台路径，可以同名
    slug          text        NOT NULL,
    status        text        NOT NULL DEFAULT 'draft',
    visibility    text        NOT NULL DEFAULT 'public',
    -- 内容三字段（agent.md §3.3）：raw 原稿、content 渲染后 HTML、raw_type 原稿格式；主题只消费 content
    raw_type      text        NOT NULL DEFAULT 'html',
    raw           text        NOT NULL DEFAULT '',
    content       text        NOT NULL DEFAULT '',
    excerpt       text        NOT NULL DEFAULT '',
    -- 摘要是否由正文自动生成；手写摘要后为 false，正文更新不再覆盖
    excerpt_auto  boolean     NOT NULL DEFAULT true,
    cover_url     text        NOT NULL DEFAULT '',
    pinned        boolean     NOT NULL DEFAULT false,
    -- 页面可选主题提供的 page-*.html 模板（agent.md §4.2）；空串表示默认模板
    template      text        NOT NULL DEFAULT '',
    author_id     bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    -- 已发布：首次发布时间；定时发布：计划发布时间；草稿：NULL 或上次发布时间
    published_at  timestamptz,
    trashed_at    timestamptz,
    -- 扩展元数据（SEO 覆写、主题自定义字段），插件生态预留
    meta          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT posts_type_check CHECK (type IN ('post', 'page')),
    CONSTRAINT posts_status_check CHECK (status IN ('draft', 'published', 'scheduled', 'trashed')),
    CONSTRAINT posts_visibility_check CHECK (visibility IN ('public', 'private')),
    CONSTRAINT posts_raw_type_check CHECK (raw_type IN ('html', 'markdown')),
    CONSTRAINT posts_title_length CHECK (char_length(title) BETWEEN 1 AND 256),
    CONSTRAINT posts_slug_format CHECK (slug ~ '^[^\s/]{1,128}$'),
    CONSTRAINT posts_meta_is_object CHECK (jsonb_typeof(meta) = 'object')
);

CREATE UNIQUE INDEX posts_slug_key ON posts (type, slug);
-- 前台列表：按类型与状态取，置顶优先，再按发布时间倒序
CREATE INDEX posts_list_idx ON posts (type, status, pinned DESC, published_at DESC);
CREATE INDEX posts_author_idx ON posts (author_id);
-- 定时发布扫描只关心待发布的记录
CREATE INDEX posts_scheduled_idx ON posts (published_at) WHERE status = 'scheduled';
CREATE INDEX posts_meta_gin ON posts USING GIN (meta);

-- 文章与分类、标签多对多；删除任一端时关联随之消失
CREATE TABLE post_categories (
    post_id     bigint NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    category_id bigint NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, category_id)
);
CREATE INDEX post_categories_category_idx ON post_categories (category_id);

CREATE TABLE post_tags (
    post_id bigint NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    tag_id  bigint NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (post_id, tag_id)
);
CREATE INDEX post_tags_tag_idx ON post_tags (tag_id);

-- 修订历史：每次内容变化的完整快照，按文章保留最近若干版
CREATE TABLE post_revisions (
    id          bigserial   PRIMARY KEY,
    post_id     bigint      NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    author_id   bigint      REFERENCES users (id) ON DELETE SET NULL,
    title       text        NOT NULL,
    raw_type    text        NOT NULL,
    raw         text        NOT NULL,
    content     text        NOT NULL,
    excerpt     text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX post_revisions_post_idx ON post_revisions (post_id, id DESC);

-- +goose Down
DROP TABLE post_revisions;
DROP TABLE post_tags;
DROP TABLE post_categories;
DROP TABLE posts;
