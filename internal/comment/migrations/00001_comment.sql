-- +goose Up
-- 评论。本模块的迁移编号独立于核心，从 1 起。
-- 依赖 content 模块的 posts 表：modules.go 中 content 必须先于 comment 注册。

CREATE TABLE comments (
    id          bigserial   PRIMARY KEY,
    -- 评论挂在文章或页面上，两者同为 posts 表的行
    post_id     bigint      NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    -- 回复树：删父评论时整条子树一并消失，不留悬空回复
    parent_id   bigint      REFERENCES comments (id) ON DELETE CASCADE,
    -- 已登录用户的评论带 user_id；用户注销后置空，评论本身保留
    user_id     bigint      REFERENCES users (id) ON DELETE SET NULL,
    author_name text        NOT NULL,
    -- 邮箱是个人信息：只在 Console 平面出现，前台响应绝不带上
    author_email text       NOT NULL DEFAULT '',
    author_url  text        NOT NULL DEFAULT '',
    -- content 是访客提交的原文（纯文本，不接受 HTML）
    content     text        NOT NULL,
    -- content_html 是转义并线性化后的展示版本，主题只消费它
    content_html text       NOT NULL DEFAULT '',
    status      text        NOT NULL DEFAULT 'pending',
    -- 留痕用于反垃圾与封禁，不对外暴露
    ip          text        NOT NULL DEFAULT '',
    user_agent  text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT comments_status_check CHECK (status IN ('pending', 'approved', 'spam')),
    CONSTRAINT comments_no_self_parent CHECK (parent_id IS NULL OR parent_id <> id),
    CONSTRAINT comments_author_name_length CHECK (char_length(author_name) BETWEEN 1 AND 64),
    CONSTRAINT comments_author_email_length CHECK (char_length(author_email) <= 254),
    CONSTRAINT comments_author_url_length CHECK (char_length(author_url) <= 512),
    CONSTRAINT comments_content_length CHECK (char_length(content) BETWEEN 1 AND 10000)
);

-- 前台按文章取已通过的评论，按时间正序
CREATE INDEX comments_post_idx ON comments (post_id, status, created_at);
-- 后台按状态翻审核队列
CREATE INDEX comments_status_idx ON comments (status, created_at DESC);
CREATE INDEX comments_parent_idx ON comments (parent_id);
-- 限流要按 IP 查最近一条
CREATE INDEX comments_ip_idx ON comments (ip, created_at DESC);

-- +goose Down
DROP TABLE comments;
