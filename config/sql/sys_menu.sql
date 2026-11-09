-- 业务参考: docs/系统管理-菜单·配置·字典设计.md §1/§1.1/§6（菜单 + 多语言子表）
-- 迁移幂等：CREATE TABLE IF NOT EXISTS，启动时执行。
-- action 唯一性用生成列实现（MySQL 无部分唯一索引）：
--   仅“未删除且 action_code 非空”的行参与 uk_action 去重；目录/菜单 action_code=''
--   → 生成列为 NULL，不去重、互不冲突；动作节点全局唯一（docs 约定）。
CREATE TABLE IF NOT EXISTS `sys_menu` (
    `id`          BIGINT       NOT NULL COMMENT '雪花ID，应用侧生成',
    `parent_id`   BIGINT       NOT NULL DEFAULT 0 COMMENT '父菜单；0=根',
    `name`        VARCHAR(64)  NOT NULL COMMENT '菜单名称（默认语言名；译文在 sys_menu_i18n）',
    `type`        TINYINT      NOT NULL DEFAULT 1 COMMENT '1目录 2菜单 3动作(action)',
    `path`        VARCHAR(128) NOT NULL DEFAULT '' COMMENT '前端路由路径（type=2 必填）',
    `component`   VARCHAR(128) NOT NULL DEFAULT '' COMMENT '前端组件路径',
    `icon`        VARCHAR(64)  NOT NULL DEFAULT '',
    `sort`        INT          NOT NULL DEFAULT 0,
    `visible`     TINYINT(1)   NOT NULL DEFAULT 1 COMMENT '1显示 0隐藏',
    `status`      TINYINT      NOT NULL DEFAULT 1 COMMENT '1启用 0停用',
    `action_code` VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '动作编码；type=3 必填，全局唯一（uk_action）；非动作必须为空',
    `remark`      VARCHAR(255) NOT NULL DEFAULT '',
    `deleted_at`  BIGINT       NOT NULL DEFAULT 0,
    `created_at`  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at`  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    `action_uniq` VARCHAR(64) GENERATED ALWAYS AS (
        CASE WHEN action_code <> '' AND deleted_at = 0 THEN action_code ELSE NULL END
    ) STORED,
    PRIMARY KEY (`id`),
    KEY `idx_menu_parent` (`parent_id`, `sort`),
    UNIQUE KEY `uk_action` (`action_uniq`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
