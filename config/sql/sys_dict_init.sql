-- 业务参考: docs/系统管理-字典(sys_dict)设计.md §1.2（两表 DDL）/§6（本文件约定）/§0（字典化判据）
--           docs/基础设施-数据文案多语言(i18n)设计.md §2（sys_i18n_text 表结构与索引）/§2.4（合并粒度）
--           docs/基础设施-文件存储(FileManager)设计.md §13.2.2（biz 双字段澄清与字典化取舍）
-- 前置: ① config/sql/sys_i18n_text.sql（公共译文表，已交付）；② sys_dict_type.sql / sys_dict_item.sql（两表建表，已交付，迁移器一条一文件）。
--       本文件由 migrate.go seedDict 在结构迁移全部完成后逐条执行（多语句文件，引号感知拆分）。
-- 幂等: INSERT IGNORE，依赖唯一键 (code,deleted_at) / (type_id,value,deleted_at) / uk_i18n(res_type,res_id,field,deleted_at)
--       ——重复执行自动跳过；用 IGNORE 而非 ON DUPLICATE KEY UPDATE，避免覆盖运营在后台改过的 label/sort/status。
-- ID 约定: seed 固定段 9000000000000001 起（9e15）。
--       bwmarrin/snowflake 生成的 ID 在 1e17+ 量级，永不冲突；段内按类型分块，见各行注释。
-- 译文粒度: sys_i18n_text 为合并粒度（一行 = 资源×字段，全部语言进 texts JSON + lang_mask 位掩码）；
--       **默认语言（zh-CN）不进译文表**（§7.4：主表 label 已是默认语言唯一真相）。
--
-- 字典化取舍（FileManager §13.2.2）：判断标准只有一句——这份文案谁维护？
--   ✅ 走字典：值域随业务增长的开放枚举（import_type）——新增导入功能插一条即可，无需发版；
--   ❌ 不走字典：封闭代码枚举（file.biz / file.driver / 文件状态 / import_batch.status）——
--      文案归开发、随代码发版，走前端语言包（i18n key，如 file.biz.avatar）：三种语言随代码一次性就位，
--      不需要运营为每个枚举补译文，也不会因为漏翻而在界面露码。
--      （字典 v1.1 起已支持 label 多语言，"英文界面 + 中文下拉"不再是拒绝理由；拒绝的理由是维护归属。）
--   若决定统一字典化，启用文末第 3 段（默认注释）。

-- ========== 1. 走字典：import_type 导入业务域（开放值域，默认启用） ==========

INSERT IGNORE INTO `sys_dict_type`
    (`id`, `code`, `name`, `status`, `remark`)
VALUES
    (9000000000000001, 'import_type', '导入业务域', 1,
     '导入批次的目标业务域；新增导入功能时在此加项，前端下拉/筛选自动出现。校验规则与处理器仍由代码实现，字典只管展示与取值列表');

INSERT IGNORE INTO `sys_dict_item`
    (`id`, `type_id`, `label`, `value`, `sort`, `status`, `remark`)
VALUES
    (9000000000000011, 9000000000000001, '员工花名册', 'employee', 10, 1, 'P1 随 FileManager 导入功能上线');
    -- 扩展方式：新导入功能上线时按 sort 递增加项，例如——
    -- (9000000000000012, 9000000000000001, '考勤记录', 'attendance', 20, 0, '考勤导入上线时改为启用'),

-- ========== 2. 字典项多语言译文（统一落 sys_i18n_text，合并粒度，默认启用） ==========
-- 只翻 label；value 永远单值。主表 label 已是默认语言（zh-CN，§7.4 不进译文表）；
-- 缺译不报错：读取时回退主表 label。lang_mask 位分配见 domain/i18n/bit.go（zh-TW=2 / en-US=4），
-- complete=0（语言集 6 门，尚缺 ja-JP/ko-KR/es-ES 时为正确状态，启动期 RecompleteI18nRows 会重算）。

INSERT IGNORE INTO `sys_i18n_text`
    (`id`, `res_type`, `res_id`, `field`, `texts`, `lang_mask`, `complete`)
VALUES
    (9000000000000031, 'dict_item', 9000000000000011, 'label',
     '{"en-US": "Employee Roster", "zh-TW": "員工名冊"}', 6, 0);
-- 扩展方式：新增字典项后若需预置译文，按 (res_id,field) 一行合并插入；
-- 或什么都不做——启动期 BackfillDictItemI18nRows 会为无行项补空行，译文运营在后台录入。

-- ========== 3. （可选，默认注释）封闭枚举统一字典化 ==========
-- 启用前提：接受这些枚举的文案由运营维护；值域校验仍以代码为准（domain/file.Biz 等常量），
-- 字典加值不会让后端接受新枚举，安全；字典删/停用项只影响前端下拉展示。
-- 注意：启用后这些文案要与前端语言包 file.bizOptions.* 保持单一来源，避免两处都改、两处不一致。

-- INSERT IGNORE INTO `sys_dict_type` (`id`, `code`, `name`, `status`, `remark`) VALUES
--     (9000000000000021, 'file_biz',      '文件用途', 1, 'sys_file.biz 固定枚举；值域以 domain/file.Biz 代码为准，字典仅用于展示与筛选'),
--     (9000000000000022, 'file_driver',   '存储驱动', 1, 'sys_file.driver；实际生效配置以 yaml storage.driver 为准，此字典只管列表展示'),
--     (9000000000000023, 'file_status',   '文件状态', 1, '由 deleted_at/purged_at 派生，非库中字段'),
--     (9000000000000024, 'import_status', '导入状态', 1, 'sys_import_batch.status 固定枚举');
--
-- INSERT IGNORE INTO `sys_dict_item` (`id`, `type_id`, `label`, `value`, `sort`, `status`, `remark`) VALUES
--     -- file_biz（值域 = domain/file.Biz 七个常量，顺序对应 §4 定义）
--     (9000000000000211, 9000000000000021, '头像',       'avatar',   10, 1, ''),
--     (9000000000000212, 9000000000000021, '简历',       'resume',   20, 1, ''),
--     (9000000000000213, 9000000000000021, '合同/证明',  'contract', 30, 1, ''),
--     (9000000000000214, 9000000000000021, '导入源文件', 'import',   40, 1, ''),
--     (9000000000000215, 9000000000000021, '导出结果',   'export',   50, 1, ''),
--     (9000000000000216, 9000000000000021, '站点资源',   'site',     60, 1, 'logo/背景图等可公开资源'),
--     (9000000000000217, 9000000000000021, '临时文件',   'temp',     70, 1, ''),
--     -- file_driver
--     (9000000000000221, 9000000000000022, '本地存储',   'local', 10, 1, ''),
--     (9000000000000222, 9000000000000022, '阿里云 OSS', 'oss',   20, 1, ''),
--     (9000000000000223, 9000000000000022, 'MinIO',      'minio', 30, 1, ''),
--     -- file_status（deleted_at=0 正常；deleted_at>0 且 purged_at IS NULL 回收站；purged_at 非空已清除）
--     (9000000000000231, 9000000000000023, '正常',   'normal',  10, 1, ''),
--     (9000000000000232, 9000000000000023, '回收站', 'trashed', 20, 1, ''),
--     (9000000000000233, 9000000000000023, '已清除', 'purged',  30, 1, '墓碑行，不可恢复'),
--     -- import_status
--     (9000000000000241, 9000000000000024, '已解析',   'parsed',    10, 1, ''),
--     (9000000000000242, 9000000000000024, '校验通过', 'validated', 20, 1, ''),
--     (9000000000000243, 9000000000000024, '校验失败', 'failed',    30, 1, 'error_detail 非空，见 FileManager §13'),
--     (9000000000000244, 9000000000000024, '已导入',   'imported',  40, 1, '');
