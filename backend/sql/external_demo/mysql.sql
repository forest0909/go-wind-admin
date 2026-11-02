-- 外部数据示例表（MySQL 8）。在外部库里手动执行，系统不会自动建表。
-- 时间列无时区，按配置 data.external_sources.demo.timezone 解释。
-- 排序规则用 _ci，contains 搜索才不区分大小写。
CREATE TABLE ext_demo_contacts (
    id         VARCHAR(36)    NOT NULL PRIMARY KEY,
    name       VARCHAR(100)   NOT NULL,
    code       VARCHAR(50),
    is_active  TINYINT(1)     NOT NULL DEFAULT 1,
    amount     DECIMAL(18, 2),
    remark     VARCHAR(500),
    created_at DATETIME(6)    NOT NULL,
    updated_at DATETIME(6)    NOT NULL
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
