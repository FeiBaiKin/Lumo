-- +goose Up
-- 前台自助注册（agent.md §7.1，2026-09-14 推翻了先前「不开放访客注册」的结论）。
--
-- 这里只动核心的 users 表：两种一次性令牌（邮箱验证、密码重置）属于 account 模块，
-- 建在 internal/account/migrations/ 下，不占核心的版本号。

ALTER TABLE users ADD COLUMN email_verified_at timestamptz;

-- 既有账号一律视为已验证。
-- 不回填就会把现有的全部管理员锁在门外（Service.Login 的闸门会拒绝 NULL），
-- 这是本次改动唯一能造成生产事故的一步，故与加列写在同一对迁移里。
UPDATE users SET email_verified_at = now() WHERE email_verified_at IS NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN email_verified_at;
