-- 业务参考: docs/系统管理-菜单·配置·字典设计.md §1.1/§6
-- 只翻 name：path/component/icon/action_code 绝不进这张表（翻了路由/权限全失效）。
-- 语言集合从 pkg/i18n.Registered() 派生；写入前经 i18n.ParseLang 归一化。
CREATE TABLE IF NOT EXISTS `sys_menu_i18n` (
    `id`         BIGINT      NOT NULL COMMENT '雪花ID，应用侧生成',
    `menu_id`    BIGINT      NOT NULL COMMENT '所属菜单',
    `lang`       VARCHAR(16) NOT NULL COMMENT '已归一化的语言标签：zh-CN / en-US',
    `name`       VARCHAR(64) NOT NULL COMMENT '该语言下的菜单名',
    `deleted_at` BIGINT      NOT NULL DEFAULT 0,
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    -- 软删后同名语言可重建；同一菜单同一语言仅一行
    UNIQUE KEY `uk_menu_i18n` (`menu_id`, `lang`, `deleted_at`),
    KEY `idx_menu_i18n_menu` (`menu_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
