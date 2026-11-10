-- 业务参考: docs/基础设施-数据文案多语言(i18n)设计.md §2（表设计与索引次序）
-- 作用: 数据文案（运营维护的展示文案）统一翻译表，取代 sys_menu_i18n / sys_dict_item_i18n
-- 执行顺序: 本文件 → 回填（旧表数据迁移，见设计文档 §8）→ 切换代码读路径 → 旧表保留一个版本后 DROP
-- 约定: 软删 deleted_at 参与唯一键；写入空串等价于软删该语言；语言标签写入前经 i18n.ParseLang 归一化

CREATE TABLE IF NOT EXISTS `sys_i18n_text` (
    `id`         BIGINT       NOT NULL COMMENT '雪花ID，应用侧生成',
    `res_type`   VARCHAR(32)  NOT NULL COMMENT '资源类型：menu / dict_item / dept / ...（domain/i18n 常量白名单）',
    `res_id`     BIGINT       NOT NULL COMMENT '资源主键',
    `field`      VARCHAR(32)  NOT NULL COMMENT '可翻字段：name / label / title（各资源的白名单内）',
    `lang`       VARCHAR(16)  NOT NULL COMMENT '已归一化语言标签：zh-CN / zh-TW / en-US',
    `content`    VARCHAR(512) NOT NULL COMMENT '译文；写入空串等价于删除该语言',
    `deleted_at` BIGINT       NOT NULL DEFAULT 0,
    `created_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    -- 一个资源的某字段在某语言下只有一行；软删后同语言可重建
    UNIQUE KEY `uk_i18n` (`res_type`, `res_id`, `field`, `lang`, `deleted_at`),
    -- 列表页按 (资源类型 + 字段 + 语言) 批量取一页译文
    KEY `idx_i18n_batch` (`res_type`, `field`, `lang`, `res_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
