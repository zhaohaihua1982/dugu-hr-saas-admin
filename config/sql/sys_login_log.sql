-- 业务参考: docs/SaaS管理后台-管理账号(sys_user)设计.md §3.2
-- 迁移幂等：CREATE TABLE IF NOT EXISTS，启动时执行。
CREATE TABLE IF NOT EXISTS `sys_login_log` (
    `id`              BIGINT        NOT NULL COMMENT '雪花ID，应用侧生成',
    `user_id`         BIGINT        NOT NULL DEFAULT 0 COMMENT '解析失败时为 0',
    `identifier_mask` VARCHAR(64)   NOT NULL COMMENT '脱敏后的输入：ad***in / 138****8000',
    `login_type`      VARCHAR(16)   NOT NULL COMMENT 'PASSWORD / SMS',
    `ident_via`       VARCHAR(16)   NOT NULL DEFAULT '' COMMENT '命中的标识：USERNAME / MOBILE',
    `success`         TINYINT(1)    NOT NULL,
    `fail_reason`     VARCHAR(32)   NOT NULL DEFAULT '' COMMENT '固定枚举，见 consts',
    `ip`              VARCHAR(45)   NOT NULL DEFAULT '',
    `ua`              VARCHAR(512)  NOT NULL DEFAULT '',
    `created_at`      DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    KEY `idx_log_user` (`user_id`, `created_at`),
    KEY `idx_log_time` (`created_at`),
    KEY `idx_log_ip`   (`ip`, `created_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
