-- 外部数据示例表（PostgreSQL）。在外部库里手动执行，系统不会自动建表。
-- 时间列无时区，按配置 data.external_sources.demo.timezone 解释。
CREATE TABLE ext_demo_contacts (
    id         VARCHAR(36)    NOT NULL PRIMARY KEY,
    name       VARCHAR(100)   NOT NULL,
    code       VARCHAR(50),
    is_active  BOOLEAN        NOT NULL DEFAULT TRUE,
    amount     DECIMAL(18, 2),
    remark     VARCHAR(500),
    created_at TIMESTAMP      NOT NULL,
    updated_at TIMESTAMP      NOT NULL
);
