-- +goose Up
-- 站长授予过的能力（spec.capabilities 的规范化副本）。启用带能力声明的插件时写入；
-- 升级后清单多要了能力、而这里没有，插件就先停用，等站长再次确认。NULL 表示从没授予过。
ALTER TABLE plugins ADD COLUMN granted jsonb;

-- 插件被系统停用的原因：连续崩溃、升级后多要了能力、后端加载失败。
-- 站长手动停用时为空串；后台据此告诉站长「它为什么停了」。
ALTER TABLE plugins ADD COLUMN disabled_reason text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE plugins DROP COLUMN disabled_reason;
ALTER TABLE plugins DROP COLUMN granted;
