-- +goose Up
-- 认证与权限（agent.md §7）。

-- 用户表。v1 不开访客注册，用户由管理员创建（agent.md §7.1）。
CREATE TABLE users (
    id            bigserial   PRIMARY KEY,
    username      text        NOT NULL,
    email         text        NOT NULL,
    -- argon2id PHC 格式哈希，明文密码不落库
    password_hash text        NOT NULL,
    display_name  text        NOT NULL DEFAULT '',
    avatar_url    text        NOT NULL DEFAULT '',
    bio           text        NOT NULL DEFAULT '',
    -- 停用的账号保留数据但不能登录，优于直接删除（其内容仍需归属）
    disabled      boolean     NOT NULL DEFAULT false,
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    -- 用户名为 DNS-1123 风格：小写字母数字与连字符，便于作为作者页 URL 片段
    CONSTRAINT users_username_format CHECK (username ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    CONSTRAINT users_username_length CHECK (char_length(username) BETWEEN 2 AND 64),
    CONSTRAINT users_email_format CHECK (position('@' IN email) > 1)
);

-- 用户名与邮箱大小写不敏感唯一：避免 Alice 与 alice 同时存在造成登录歧义
CREATE UNIQUE INDEX users_username_key ON users (lower(username));
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

-- 角色表。内置角色由程序保证存在，builtin 标记防止被删除
-- （2026-09-15 起内置角色的**权限**可改，见 00004；名字与删除仍然受保护）。
CREATE TABLE roles (
    id          bigserial   PRIMARY KEY,
    name        text        NOT NULL,
    label       text        NOT NULL DEFAULT '',
    description text        NOT NULL DEFAULT '',
    -- 权限串集合，存为 jsonb 数组以便整体读写
    permissions jsonb       NOT NULL DEFAULT '[]'::jsonb,
    builtin     boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT roles_name_format CHECK (name ~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'),
    CONSTRAINT roles_permissions_is_array CHECK (jsonb_typeof(permissions) = 'array')
);

CREATE UNIQUE INDEX roles_name_key ON roles (name);

-- 用户与角色多对多。
CREATE TABLE user_roles (
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id     bigint      NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    granted_at  timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, role_id)
);

CREATE INDEX user_roles_role_idx ON user_roles (role_id);

-- 服务端会话。Cookie 里只放会话令牌的哈希对应的明文，库中仅存哈希。
CREATE TABLE sessions (
    -- 会话令牌的 SHA-256 哈希（十六进制）。令牌明文只存在于 Cookie 中，
    -- 库被读取也无法反推出可用凭据。
    token_hash  text        PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- CSRF 令牌与会话绑定，走双提交校验
    csrf_token  text        NOT NULL,
    user_agent  text        NOT NULL DEFAULT '',
    ip          text        NOT NULL DEFAULT '',
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    -- 滑动过期需要记录最近活跃时间
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_user_idx ON sessions (user_id);
-- 清理过期会话的定期任务按此索引扫描
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- Personal Access Token，供无头调用（agent.md §7.1：仅存哈希）。
CREATE TABLE access_tokens (
    id          bigserial   PRIMARY KEY,
    -- 令牌的 SHA-256 哈希（十六进制），明文仅在创建时返回一次
    token_hash  text        NOT NULL,
    -- 令牌前缀，用于在 UI 中区分展示（如 lumo_pat_ab12…），不足以用于认证
    token_hint  text        NOT NULL DEFAULT '',
    user_id     bigint      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name        text        NOT NULL,
    -- 令牌可持有用户权限的子集；空数组表示继承用户全部权限
    scopes      jsonb       NOT NULL DEFAULT '[]'::jsonb,
    -- NULL 表示永不过期
    expires_at  timestamptz,
    last_used_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT access_tokens_scopes_is_array CHECK (jsonb_typeof(scopes) = 'array'),
    CONSTRAINT access_tokens_name_length CHECK (char_length(name) BETWEEN 1 AND 128)
);

CREATE UNIQUE INDEX access_tokens_hash_key ON access_tokens (token_hash);
CREATE INDEX access_tokens_user_idx ON access_tokens (user_id);

-- +goose Down
DROP TABLE access_tokens;
DROP TABLE sessions;
DROP TABLE user_roles;
DROP TABLE roles;
DROP TABLE users;
