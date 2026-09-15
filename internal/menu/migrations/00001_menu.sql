-- +goose Up
-- 菜单。本模块的迁移编号独立于核心，从 1 起。

CREATE TABLE menus (
    id          bigserial   PRIMARY KEY,
    name        text        NOT NULL,
    -- slug 是主题引用菜单的键（{{ menus.tree "main" }}），故全局唯一
    slug        text        NOT NULL,
    description text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT menus_name_length CHECK (char_length(name) BETWEEN 1 AND 64),
    CONSTRAINT menus_slug_format CHECK (slug ~ '^[^\s/]{1,128}$')
);
CREATE UNIQUE INDEX menus_slug_key ON menus (slug);

CREATE TABLE menu_items (
    id          bigserial   PRIMARY KEY,
    menu_id     bigint      NOT NULL REFERENCES menus (id) ON DELETE CASCADE,
    -- 邻接表；删除父项时子项一并消失，避免留下悬空的二级菜单
    parent_id   bigint      REFERENCES menu_items (id) ON DELETE CASCADE,
    position    integer     NOT NULL DEFAULT 0,
    label       text        NOT NULL,
    -- type 决定 url 是自动解析还是手填：
    --   custom   用 url 字段
    --   post/page/category/tag 用 target_id 指向对应记录，url 由服务端解析
    type        text        NOT NULL DEFAULT 'custom',
    target_id   bigint,
    url         text        NOT NULL DEFAULT '',
    -- 新窗口打开与 rel 属性
    target      text        NOT NULL DEFAULT '',
    rel         text        NOT NULL DEFAULT '',
    -- 隐藏的条目不出现在前台，但仍保留在后台树里（临时下线某入口的常见需求）
    visible     boolean     NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT menu_items_type_check CHECK (type IN ('custom', 'post', 'page', 'category', 'tag')),
    CONSTRAINT menu_items_label_length CHECK (char_length(label) BETWEEN 1 AND 128),
    CONSTRAINT menu_items_target_check CHECK (target IN ('', '_blank')),
    CONSTRAINT menu_items_url_length CHECK (char_length(url) <= 1024),
    CONSTRAINT menu_items_no_self_parent CHECK (parent_id IS NULL OR parent_id <> id),
    -- 内部关联型条目必须有 target_id；自定义链接必须有 url
    CONSTRAINT menu_items_target_id_required CHECK (type = 'custom' OR target_id IS NOT NULL),
    CONSTRAINT menu_items_url_required CHECK (type <> 'custom' OR url <> '')
);
CREATE INDEX menu_items_menu_idx ON menu_items (menu_id, parent_id, position);
CREATE INDEX menu_items_parent_idx ON menu_items (parent_id);

-- +goose Down
DROP TABLE menu_items;
DROP TABLE menus;
