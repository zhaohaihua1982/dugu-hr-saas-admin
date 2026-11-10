-- 业务参考: docs/系统管理-菜单·配置·字典设计.md §1/§1.1/§6（菜单 + 多语言子表）
-- 迁移幂等：CREATE TABLE IF NOT EXISTS，启动时执行。
-- 同级唯一（v0.4 终稿）：唯一索引直接建在 (parent_id, <col>, deleted_at) 复合列上
--   （与 sys_user/sys_dict 同款“软删参与唯一键”手法，无生成列多余字段）：
--   活行 deleted_at=0 互相排斥重名/同址/同码；软删行 deleted_at=毫秒戳互不重复，不占坑可复用。
CREATE TABLE IF NOT EXISTS `sys_menu` (
    `id`          BIGINT       NOT NULL COMMENT '雪花ID，应用侧生成',
    `parent_id`   BIGINT       NOT NULL DEFAULT 0 COMMENT '父菜单；0=根',
    `name`        VARCHAR(64)  NOT NULL COMMENT '菜单名称（默认语言兜底列，NOT NULL；译文在统一表 sys_i18n_text）',
    `type`        TINYINT      NOT NULL DEFAULT 1 COMMENT '1目录 2菜单 3动作(action)',
    `path`        VARCHAR(128) NOT NULL DEFAULT '' COMMENT '前端路由路径；全节点必填（按钮=action_code 同值）',
    `component`   VARCHAR(128) NOT NULL DEFAULT '' COMMENT '前端组件路径',
    `icon`        VARCHAR(64)  NOT NULL DEFAULT '',
    `sort`        INT          NOT NULL DEFAULT 0,
    `visible`     TINYINT(1)   NOT NULL DEFAULT 1 COMMENT '1显示 0隐藏',
    `status`      TINYINT      NOT NULL DEFAULT 1 COMMENT '1启用 0停用',
    `code`        VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '名称前缀：目录=可选自动 dir:<id>；菜单/动作=权限点(必填)',
    `remark`      VARCHAR(255) NOT NULL DEFAULT '',
    `deleted_at`  BIGINT       NOT NULL DEFAULT 0,
    `created_at`  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at`  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    KEY `idx_menu_parent` (`parent_id`, `sort`),
    UNIQUE KEY `uk_menu_code` (`type`, `code`, `deleted_at`),
    UNIQUE KEY `uk_menu_name`   (`parent_id`, `name`, `deleted_at`),
    UNIQUE KEY `uk_menu_path`   (`parent_id`, `path`, `deleted_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
