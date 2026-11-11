-- 业务参考: docs/系统管理-字典(sys_dict)设计.md §1.2（DDL）
-- 作用: 字典项表；label 多语言不在此表——统一存 sys_i18n_text（res_type='dict_item'、field='label'）。
--       前置: sys_dict_type.sql 已建类型表；sys_i18n_text.sql 已建公共译文表。
-- 约定: 软删 deleted_at 参与唯一键（删后可重建同 value）；雪花 ID 应用侧生成。
-- 值域分工: value 是业务落库值（单值，语言无关）；label 只用于渲染（默认语言列 + 统一表译文）。

CREATE TABLE IF NOT EXISTS `sys_dict_item` (
    `id`         BIGINT       NOT NULL COMMENT '雪花ID，应用侧生成',
    `type_id`    BIGINT       NOT NULL COMMENT '所属类型 id',
    `label`      VARCHAR(64)  NOT NULL COMMENT '默认语言显示名（同时是兜底，NOT NULL 保证永不为空）',
    `value`      VARCHAR(64)  NOT NULL COMMENT '取值；业务落库与前端判断都用它，多语言不改变它',
    `sort`       INT          NOT NULL DEFAULT 0,
    `status`     TINYINT      NOT NULL DEFAULT 1 COMMENT '1启用 0停用',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '',
    `deleted_at` BIGINT       NOT NULL DEFAULT 0,
    `created_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_dict_item` (`type_id`, `value`, `deleted_at`),
    KEY `idx_dict_item_type` (`type_id`, `sort`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
