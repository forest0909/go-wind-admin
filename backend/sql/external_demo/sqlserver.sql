-- 外部数据示例表（SQL Server）。在外部库里手动执行，系统不会自动建表。
-- 时间列无时区，按配置 data.external_sources.demo.timezone 解释。
-- 文本列用 NVARCHAR 存中文；contains 搜索按库/列的排序规则（默认 _CI_ 不区分大小写）。
CREATE TABLE ext_demo_contacts (
    id         VARCHAR(36)    NOT NULL PRIMARY KEY,
    name       NVARCHAR(100)  NOT NULL,
    code       NVARCHAR(50),
    is_active  BIT            NOT NULL DEFAULT 1,
    amount     DECIMAL(18, 2),
    remark     NVARCHAR(500),
    created_at DATETIME2(6)   NOT NULL,
    updated_at DATETIME2(6)   NOT NULL
);
