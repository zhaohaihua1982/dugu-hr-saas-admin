-- 业务参考: docs/系统管理-字典(sys_dict)设计.md §1.2（DDL）/§1.3（唯一键三条约定）
-- 作用: 字典类型 + 字典项两张业务表；label 多语言不在此表——统一存
--       sys_i18n_text（res_type='dict_item'、field='label'，见 sys_i18n_text.sql）。
-- 约定: 软删 deleted_at 参与唯一键（删后可重建同 code / 同 value）；雪花 ID 应用侧生成。
-- 值域分工: value 是业务落库值（单值，语言无关）；label 只用于渲染（默认语言列 + 统一表译文）。

CREATE TABLE IF NOT EXISTS `sys_dict_type` (
    `id`         BIGINT      NOT NULL COMMENT '雪花ID，应用侧生成',
    `code`       VARCHAR(64) NOT NULL COMMENT '类型编码，全局唯一；前端取值的唯一入口',
    `name`       VARCHAR(64) NOT NULL COMMENT '类型名称，后台列表展示用',
    `status`     TINYINT     NOT NULL DEFAULT 1 COMMENT '1启用 0停用',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '',
    `deleted_at` BIGINT      NOT NULL DEFAULT 0,
    `created_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_dict_type` (`code`, `deleted_at`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
