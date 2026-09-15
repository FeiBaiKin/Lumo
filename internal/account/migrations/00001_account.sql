-- +goose Up
-- 邮箱验证与密码重置的一次性令牌。
--
-- 两种用途合一张表：字段完全相同，签发 / 查验 / 单次消费 / 过期清理的逻辑也完全相同，
-- 分两张表只会把同样的代码写两遍。用途用 CHECK 钉死，拼错的 purpose 进不来。
CREATE TABLE account_tokens (
    -- 只存 SHA-256：明文只出现在那封邮件的链接里，库泄漏不等于账号失守
    -- （与 sessions、access_tokens 同一策略）。
    token_hash text        PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text        NOT NULL,
    expires_at timestamptz NOT NULL,
    -- 消费即写时间戳而不是删行：留痕才能回答「这个链接是不是被用过了」。
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT account_tokens_purpose CHECK (purpose IN ('verify_email', 'reset_password'))
);

CREATE INDEX account_tokens_user_idx ON account_tokens (user_id, purpose);
CREATE INDEX account_tokens_expires_idx ON account_tokens (expires_at);

-- +goose Down
DROP TABLE account_tokens;
