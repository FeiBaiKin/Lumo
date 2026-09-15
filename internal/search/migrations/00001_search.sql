-- +goose Up
-- 全文搜索索引。切词在 Go 侧完成（二元组，见 tokenize.go），
-- 这里只存结果，故不依赖任何 PostgreSQL 扩展；文本配置固定用 'simple'——
-- 它只做小写化，不做词干还原、不去停用词，正好是「词元已经切好」时想要的行为。
--
-- 索引单独成表，不往 posts 上加列：content 的插入走 bun 的 Returning("*")，
-- posts 多出一列就会被映射回 Post 结构体并报「没有这个字段」，发文当场 500。
-- 独立表还顺带让「换成 meilisearch 就整张表删掉」成为一步操作。

CREATE TABLE post_search (
    -- 随内容删除而消失，不留孤儿行
    post_id    bigint      PRIMARY KEY REFERENCES posts (id) ON DELETE CASCADE,
    -- 标题 A、摘要 B、正文 D 三段加权合并
    tsv        tsvector    NOT NULL,
    -- 建索引时所依据的 posts.updated_at；比它新的修改即为待重建
    indexed_at timestamptz NOT NULL
);

-- 检索用：@@ 匹配走 GIN。
CREATE INDEX post_search_tsv_idx ON post_search USING gin (tsv);

-- +goose Down
DROP TABLE IF EXISTS post_search;
