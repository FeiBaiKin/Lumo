-- +goose Up
-- 附件。本模块的迁移编号独立于核心，从 1 起。

CREATE TABLE media (
    id            bigserial   PRIMARY KEY,
    -- 存储用的随机化文件名，不含目录；原始名只作展示，永远不参与路径拼接
    filename      text        NOT NULL,
    original_name text        NOT NULL,
    mime          text        NOT NULL,
    -- 由 MIME 推导的粗分类，落库以便直接按等值条件筛选与建索引
    kind          text        NOT NULL,
    size          bigint      NOT NULL,
    -- 图片的原始像素尺寸；非图片与未解码的图片（如 SVG）为 0
    width         integer     NOT NULL DEFAULT 0,
    height        integer     NOT NULL DEFAULT 0,
    -- 写入时所用的存储驱动与对象键：切换驱动后旧记录仍能定位到原文件
    driver        text        NOT NULL,
    storage_key   text        NOT NULL,
    url           text        NOT NULL,
    -- 缩略图数组：[{name,key,url,width,height,size}]
    thumbnails    jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- 原文件的 SHA-256，供去重与完整性核对
    checksum      text        NOT NULL DEFAULT '',
    uploader_id   bigint      NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    alt           text        NOT NULL DEFAULT '',
    title         text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT media_kind_check CHECK (kind IN ('image', 'video', 'audio', 'document', 'other')),
    CONSTRAINT media_driver_format CHECK (driver ~ '^[a-z0-9-]{1,32}$'),
    CONSTRAINT media_size_check CHECK (size >= 0),
    CONSTRAINT media_dimensions_check CHECK (width >= 0 AND height >= 0),
    -- 文件名与对象键的形态在库层面再设一道防线：即便服务层出错也进不来带 .. 或反斜杠的键
    CONSTRAINT media_filename_format CHECK (filename ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'),
    CONSTRAINT media_storage_key_format CHECK (storage_key ~ '^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$'),
    CONSTRAINT media_storage_key_no_dotdot CHECK (storage_key !~ '\.\.'),
    CONSTRAINT media_thumbnails_is_array CHECK (jsonb_typeof(thumbnails) = 'array')
);

-- 同一驱动内对象键唯一：随机键重复即视为冲突，宁可失败也不覆盖既有文件
CREATE UNIQUE INDEX media_storage_key_key ON media (driver, storage_key);
-- 附件库默认按上传时间倒序浏览
CREATE INDEX media_created_idx ON media (created_at DESC, id DESC);
CREATE INDEX media_kind_idx ON media (kind, created_at DESC);
CREATE INDEX media_uploader_idx ON media (uploader_id, created_at DESC);
CREATE INDEX media_checksum_idx ON media (checksum);

-- +goose Down
DROP TABLE media;
