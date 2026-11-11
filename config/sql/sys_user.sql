-- 业务参考: docs/SaaS管理后台-管理账号(sys_user)设计.md §3.1
-- 迁移幂等：CREATE TABLE IF NOT EXISTS，启动时执行。
CREATE TABLE IF NOT EXISTS `sys_user` (
    `id`             BIGINT        NOT NULL COMMENT '雪花ID，应用侧生成',
    `username`       VARCHAR(64)   NOT NULL COMMENT '登录账号；禁止 11 位纯数字，防与手机号歧义',
    `password_hash`  VARCHAR(128)  NOT NULL COMMENT 'bcrypt；空串=无密码（仅短信登录）',
    `password_algo`  VARCHAR(16)   NOT NULL DEFAULT 'bcrypt' COMMENT 'bcrypt/sha256（旧算法迁移用）',
    `salt`           VARCHAR(64)   NOT NULL DEFAULT '' COMMENT '仅旧算法兼容；bcrypt 自带盐，此列留空',
    `name`           VARCHAR(64)   NOT NULL,
    `mobile_hash`    CHAR(64)      NOT NULL COMMENT 'HMAC-SHA256(归一化手机号)，登录查询入口',
    `mobile_enc`     VARCHAR(256)  NOT NULL COMMENT 'AES-GCM 密文，发短信/核对时解密',
    `mobile_mask`    VARCHAR(20)   NOT NULL COMMENT '138****8000，列表展示',
    `email`          VARCHAR(128)  NOT NULL DEFAULT '',
    `status`         TINYINT       NOT NULL DEFAULT 1 COMMENT '1正常 2停用 3锁定',
    `fail_count`     SMALLINT      NOT NULL DEFAULT 0,
    `locked_until`   DATETIME(3)   NULL,
    `last_login_at`  DATETIME(3)   NULL,
    `last_login_ip`  VARCHAR(45)   NOT NULL DEFAULT '',
    `pwd_updated_at` DATETIME(3)   NULL,
    `pwd_force_change` TINYINT(1)  NOT NULL DEFAULT 0 COMMENT '首登强制改密标记：管理员代建/重置后置位（pwd_force_first_change=1 时），改密成功清除',
    `deleted_at`     BIGINT        NOT NULL DEFAULT 0,
    `created_at`     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at`     DATETIME(3)   NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_username` (`username`, `deleted_at`),
    UNIQUE KEY `uk_mobile`   (`mobile_hash`, `deleted_at`),
    KEY `idx_user_status` (`status`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
