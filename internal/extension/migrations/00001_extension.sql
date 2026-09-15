-- +goose Up
-- Extension 平面按 URL 里的复数段寻址，而核心表 extensions 只存单数 kind。
-- kind 到复数段的映射在 Go 侧（resource.go）且不可逆——Movie 与 Movy 都得到 movies——
-- 所以把映射结果存成一列：数据库不必知道规则，按地址取记录也能走索引。

ALTER TABLE extensions ADD COLUMN resource text NOT NULL DEFAULT '';

-- 回填。本列加上之前 Extension 平面尚无任何写入方，实际上是空表；
-- 这段 CASE 与 Go 侧 Resource() 的分支逐条对应，只作兜底。
UPDATE extensions SET resource = CASE
    WHEN lower(kind) ~ '(s|x|z|ch|sh)$' THEN lower(kind) || 'es'
    WHEN lower(kind) ~ '[^aeiou]y$'     THEN left(lower(kind), -1) || 'ies'
    ELSE lower(kind) || 's'
END;

ALTER TABLE extensions ALTER COLUMN resource DROP DEFAULT;
ALTER TABLE extensions ADD CONSTRAINT extensions_resource_format CHECK (resource ~ '^[a-z][a-z0-9]*$');

-- 同一 (分组, 版本, 复数段) 下名称唯一。核心迁移的唯一约束建在 kind 上，
-- 而只差大小写的两个 kind（Replicaset 与 ReplicaSet）会落到同一个 URL 段，
-- 少了这道约束时一个地址会命中两行。该索引同时服务列表与单条查询。
CREATE UNIQUE INDEX extensions_resource_name_key ON extensions (api_group, version, resource, name);

-- +goose Down
DROP INDEX IF EXISTS extensions_resource_name_key;
ALTER TABLE extensions DROP CONSTRAINT IF EXISTS extensions_resource_format;
ALTER TABLE extensions DROP COLUMN IF EXISTS resource;
