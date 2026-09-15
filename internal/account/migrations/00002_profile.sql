-- +goose Up
-- 个人中心的封面图（agent.md §7.1 的前台个人中心）。
--
-- 与 avatar_url 同一策略：**只存 URL，文件不在这个库里**。
-- 上传走媒体模块的既有管线（类型嗅探、缩略图、本地或 S3 由存储设置决定），
-- 这里存的是它返回的地址。存 BLOB 会让备份体积随会员数线性膨胀，
-- 而这些东西本来就有自己的存储层与 CDN 出口。
--
-- 默认空串而不是 NULL：与 display_name / avatar_url 一致，
-- 「没设置」这件事用空值表达，读的地方不必处处判 NULL。
ALTER TABLE users ADD COLUMN banner_url text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE users DROP COLUMN banner_url;
