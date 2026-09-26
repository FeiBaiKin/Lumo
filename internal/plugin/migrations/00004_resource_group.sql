-- +goose Up
-- 插件资源的分组从 <插件>.plugin.lumo.run 改成 io.github.feibaikin.lumo.plugin.<插件>。
-- 新分组下已有同名记录（只可能是经 Extension 平面手工建的）时跳过，不让迁移失败挡住启动。
UPDATE extensions AS e
SET api_group = 'io.github.feibaikin.lumo.plugin.' || left(e.api_group, length(e.api_group) - length('.plugin.lumo.run'))
WHERE e.api_group LIKE '%.plugin.lumo.run'
  AND NOT EXISTS (
      SELECT 1 FROM extensions AS n
      WHERE n.api_group = 'io.github.feibaikin.lumo.plugin.' || left(e.api_group, length(e.api_group) - length('.plugin.lumo.run'))
        AND n.version = e.version AND n.resource = e.resource AND n.name = e.name
  );

-- +goose Down
UPDATE extensions AS e
SET api_group = substr(e.api_group, length('io.github.feibaikin.lumo.plugin.') + 1) || '.plugin.lumo.run'
WHERE e.api_group LIKE 'io.github.feibaikin.lumo.plugin.%'
  AND NOT EXISTS (
      SELECT 1 FROM extensions AS o
      WHERE o.api_group = substr(e.api_group, length('io.github.feibaikin.lumo.plugin.') + 1) || '.plugin.lumo.run'
        AND o.version = e.version AND o.resource = e.resource AND o.name = e.name
  );
