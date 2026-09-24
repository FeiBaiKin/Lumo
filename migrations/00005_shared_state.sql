-- +goose Up
-- 多实例部署要共享的两类运行时状态。原先都在进程内存里，每个实例各记一份。

-- 固定窗口限流计数（登录失败、注册、找回密码、上传等）。
-- UNLOGGED：计数丢了只是限流窗口提前重置，不值得为它写 WAL；
-- 数据库崩溃恢复后表被清空，这正是可以接受的代价。
CREATE UNLOGGED TABLE rate_limits (
    key        text        PRIMARY KEY,
    count      integer     NOT NULL,
    expires_at timestamptz NOT NULL
);

CREATE INDEX rate_limits_expires_at_idx ON rate_limits (expires_at);

-- 周期任务的最近执行时间。多个实例的定时器都会触发，
-- 谁先把这一行推进到「本轮」谁执行，其余实例跳过。
CREATE TABLE job_runs (
    name        text        PRIMARY KEY,
    last_run_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE job_runs;
DROP TABLE rate_limits;
