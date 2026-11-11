-- 业务参考: docs/系统管理-登录配置设计.md §3.2
-- 固定 ID seed，INSERT IGNORE 幂等（项目约定）；单条 INSERT 两行（迁移器每文件整体单语句执行）。
INSERT IGNORE INTO sys_login_config
    (id, config_type, scope_id, login_mask, fail_threshold, lock_minutes, unlock_mode,
     captcha_after_fails, ip_fail_limit, ip_rate_limit,
     access_token_ttl, refresh_token_ttl, session_hard_ttl, session_max_concurrent,
     kick_on_pwd_change, remember_days,
     pwd_level, pwd_min_length, pwd_min_classes, pwd_forbid_username,
     pwd_max_age_days, pwd_expire_action, pwd_remind_days, pwd_history_count,
     pwd_force_first_change, allow_self_change_pwd, created_at, updated_at)
VALUES
    -- 管理后台：账号/密码 + 手机/短信（v1.6 用户裁定：mask=5；手机+密码 bit1 默认
    -- 关闭，短信登录为主通道），密码策略高档
    -- v1.1 修正：refresh_token_ttl 604800→86400——原值违反 §6 规则 4（refresh ≤ 硬上限），
    -- 与 session_hard_ttl=86400 自相矛盾；以校验规则为准（用户裁定 2026-11-11）。
    (920000000000000001, 1, 0, 5, 5, 15, 3, 3, 50, 20,
     7200, 86400, 86400, 1, 1, 0,
     3, 12, 3, 1, 180, 1, 7, 3, 0, 0, NOW(3), NOW(3)),
    -- 租户登录：组件全开（账号密码|手机密码|手机短信|邮箱密码），策略中档
    (920000000000000002, 2, 0, 15, 5, 15, 3, 3, 50, 20,
     7200, 86400, 86400, 3, 1, 0,
     2, 10, 2, 1, 0, 1, 7, 3, 0, 1, NOW(3), NOW(3));
