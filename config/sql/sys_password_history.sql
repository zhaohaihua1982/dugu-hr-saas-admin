-- 业务参考: docs/系统管理-登录配置设计.md §10（P1.5）
-- 历史密码表：改密时写入旧 hash，pwd_history_count 校验即"逐条 bcrypt 比对最近 N 行"。
-- 只增不改，按 created_at 定期清理超出窗口的旧行。
CREATE TABLE IF NOT EXISTS `sys_password_history` (
    `id`            BIGINT        NOT NULL COMMENT '雪花ID',
    `user_type`     TINYINT       NOT NULL COMMENT '1 sys_user 2 hr_account',
    `user_id`       BIGINT        NOT NULL,
    `password_hash` VARCHAR(128)  NOT NULL COMMENT 'bcrypt，与主表同算法',
    `created_at`    DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    KEY `idx_pwdhist` (`user_type`, `user_id`, `created_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
