-- 业务参考: docs/基础设施-数据文案多语言(i18n)设计.md §2（表设计与索引）/§2.4（位掩码编码）
-- 作用: 数据文案（运营维护的展示文案）统一翻译表，取代 sys_menu_i18n / sys_dict_item_i18n
-- 粒度(v1.5): 一行 = 资源 × 字段，全部语言进 texts JSON + lang_mask 位掩码 + complete 完整性标记；
--             旧粒度（一语言一行）存量由 migrate.go mergeI18nTextRows 自动合并（§9.4），幂等
-- 约定: 软删 deleted_at 参与唯一键；写入空串等价于从 JSON 移除该语言（读取回退主表列）；
--       语言标签写入前经 i18n.ParseLang 归一化；默认语言不进本表（§7.4，两份真相）
-- 位分配: lang_mask 的 bit 由 domain/i18n/bit.go langBits 固定分配（zh-CN=1 / zh-TW=2 / en-US=4），
--         只许追加、严禁重排——在中间插语言会让历史行的位含义整体错位

CREATE TABLE IF NOT EXISTS `sys_i18n_text` (
    `id`         BIGINT       NOT NULL COMMENT '雪花ID，应用侧生成',
    `res_type`   VARCHAR(32)  NOT NULL COMMENT '资源类型：menu / dict_item / dept / ...（domain/i18n 常量白名单）',
    `res_id`     BIGINT       NOT NULL COMMENT '资源主键',
    `field`      VARCHAR(32)  NOT NULL COMMENT '可翻字段：name / label / title（各资源白名单内）',
    `texts`      JSON         NOT NULL COMMENT '全部语言译文 {lang: content}；不含默认语言（§7.4）',
    `lang_mask`  BIGINT       NOT NULL DEFAULT 0 COMMENT '语言位掩码：bit=1 表示该语言已录入（位分配 domain/i18n.LangBit）',
    `complete`   TINYINT      NOT NULL DEFAULT 0 COMMENT '1=非默认语言的已注册语言全部录入（写入时重算，缺译清单 WHERE complete=0）',
    `deleted_at` BIGINT       NOT NULL DEFAULT 0,
    `created_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    `updated_at` DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (`id`),
    -- 一个资源的某字段只有一行活数据；软删后可重建
    UNIQUE KEY `uk_i18n` (`res_type`, `res_id`, `field`, `deleted_at`),
    -- 列表页按 (资源类型 + 字段) 批量取一页译文（语言维度在 JSON 内，Go 侧取键）
    KEY `idx_i18n_batch` (`res_type`, `field`, `res_id`),
    -- 缺译清单：WHERE res_type=? AND field=? AND complete=0 直接命中
    KEY `idx_i18n_incomplete` (`res_type`, `field`, `complete`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
