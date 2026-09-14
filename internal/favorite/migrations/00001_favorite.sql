-- +goose Up
-- 收藏：已登录用户把一篇内容标记下来，日后在「我的收藏」里找回。
-- 本模块的迁移编号独立于核心，从 1 起。
-- 依赖 content 模块的 posts 表：modules.go 中 content 必须先于 favorite 注册。

CREATE TABLE favorites (
    id         bigserial   PRIMARY KEY,
    -- 两端都跟着源头走：人注销了、内容删了，这一行就不再有任何意义。
    -- 这与评论相反——评论在用户注销后仍要留住正文，故那边是 SET NULL。
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- 收藏挂在文章或页面上，两者同为 posts 表的行
    post_id    bigint      NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),

    -- 同一个人对同一篇只能有一条。判重交给这条约束而不是「先查后插」：
    -- 双击一次收藏按钮发出的两个请求之间没有空隙可钻，而先查后插有。
    CONSTRAINT favorites_user_post_key UNIQUE (user_id, post_id)
);

-- 收藏页按收藏时间倒序翻页。唯一约束自带的索引以 user_id 打头，
-- 但它的次列是 post_id，取一页仍要把该用户的全部收藏排一遍，故另建一条带时间的。
CREATE INDEX favorites_user_idx ON favorites (user_id, created_at DESC);
-- 文章页要显示这篇被收藏了多少次。
CREATE INDEX favorites_post_idx ON favorites (post_id);

-- +goose Down
DROP TABLE favorites;
