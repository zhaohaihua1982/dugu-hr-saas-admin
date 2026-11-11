// Package loginconfig —— 登录配置管理用例（docs/系统管理-登录配置设计.md §5/§6）。
// GET 列表/详情（含兜底横幅）、PUT 全量更新（乐观锁 + §6 校验 + 缓存失效 + 审计 diff）、
// POST reset 恢复默认值。管线侧消费走 domain/loginconfig.Provider，不经本包。
package loginconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"dugu-hr-saas-admin/application/types"
	"dugu-hr-saas-admin/domain/authn"
	dloginconfig "dugu-hr-saas-admin/domain/loginconfig"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// ---------- DTO ----------

// UpdateReq 全量更新请求体（§5 约定 1：PUT 全量而非 PATCH——配置项之间有联动校验，
// 逐项改会出现"先改阈值触发校验失败、卡在中间态"；前端编辑弹窗一次提交整单）。
type UpdateReq struct {
	LoginMask            int64           `json:"login_mask"`
	FailThreshold        int             `json:"fail_threshold"`
	LockMinutes          int             `json:"lock_minutes"`
	UnlockMode           int8            `json:"unlock_mode" binding:"oneof=1 2 3"`
	CaptchaAfterFails    int             `json:"captcha_after_fails"`
	IPFailLimit          int             `json:"ip_fail_limit"`
	IPRateLimit          int             `json:"ip_rate_limit"`
	AccessTokenTTL       int             `json:"access_token_ttl"`
	RefreshTokenTTL      int             `json:"refresh_token_ttl"`
	SessionHardTTL       int             `json:"session_hard_ttl"`
	SessionMaxConcurrent int             `json:"session_max_concurrent"`
	KickOnPwdChange      *bool           `json:"kick_on_pwd_change"`
	RememberDays         int             `json:"remember_days"`
	PwdLevel             int8            `json:"pwd_level"` // 仅预设展示用：保存时按明细反推回写（§2.4）
	PwdMinLength         int             `json:"pwd_min_length"`
	PwdMinClasses        int             `json:"pwd_min_classes"`
	PwdForbidUsername    *bool           `json:"pwd_forbid_username"`
	PwdMaxAgeDays        int             `json:"pwd_max_age_days"`
	PwdExpireAction      int8            `json:"pwd_expire_action" binding:"omitempty,oneof=1 2"`
	PwdRemindDays        int             `json:"pwd_remind_days"`
	PwdHistoryCount      int             `json:"pwd_history_count"`
	PwdForceFirstChange  *bool           `json:"pwd_force_first_change"`
	AllowSelfChangePwd   *bool           `json:"allow_self_change_pwd"`
	Extras               json.RawMessage `json:"extras"`                  // 与库里现值合并（未知 key 保留，§8）
	Version              int             `json:"version" binding:"min=0"` // 乐观锁（seed 行初始 0，故不用 required）
}

// ConfigView 详情/列表行视图：配置全量字段 + 兜底横幅标记（§8 边界：行不存在时
// 管理端展示"当前为兜底默认值"）。
type ConfigView struct {
	*dloginconfig.Config
	Fallback bool `json:"fallback"`
}

// UpdateResp 更新成功响应（返回新版本号，§5 约定 3）。
type UpdateResp struct {
	Version int `json:"version"`
}

// ---------- 用例 ----------

// Service 登录配置管理用例。
type Service struct {
	repo     dloginconfig.Repository
	provider dloginconfig.Provider
	ids      authn.IDGenerator
}

// NewService 构造。
func NewService(repo dloginconfig.Repository, provider dloginconfig.Provider, ids authn.IDGenerator) *Service {
	return &Service{repo: repo, provider: provider, ids: ids}
}

// List 两种类型各一行（§5）。行缺失（seed 漏跑）→ 兜底默认值 + fallback=true 横幅。
func (s *Service) List(ctx context.Context) ([]ConfigView, error) {
	out := make([]ConfigView, 0, 2)
	for _, t := range []dloginconfig.ConfigType{dloginconfig.TypeAdmin, dloginconfig.TypeTenant} {
		v, err := s.view(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// Get 单条详情。
func (s *Service) Get(ctx context.Context, t dloginconfig.ConfigType) (*ConfigView, error) {
	if !t.Valid() {
		return nil, dloginconfig.ErrNotFound
	}
	return s.view(ctx, t)
}

// Update 全量更新（§5/§6）：
// 行必须已存在（缺失 → 30048，用 reset 建行）→ extras 合并（未知 key 保留）→
// §6 服务端校验（规则 1/2/3/4/5/6/8）→ 等级按明细反推（规则 7）→ 乐观锁保存 →
// 删缓存失效 → 审计 diff → 返回新版本号。
func (s *Service) Update(ctx context.Context, t dloginconfig.ConfigType, req *UpdateReq, opUID int64, p types.RequestParams) (*UpdateResp, error) {
	if !t.Valid() {
		return nil, dloginconfig.ErrNotFound
	}
	cur, err := s.repo.ByType(ctx, t)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, dloginconfig.ErrNotFound // 行缺失：先 reset 建行（§8 边界 1）
	}

	next := *cur
	applyReq(&next, req)
	next.Version = req.Version
	next.UpdatedBy = opUID

	// extras：已知 key 以提交为准，未知 key 保留（§8）
	merged, err := dloginconfig.MergeExtras(cur.Extras, req.Extras)
	if err != nil {
		return nil, err
	}
	next.Extras = merged

	// §6 服务端校验 + 规则 7 等级反推（明细字段是唯一真相）
	if err := next.Validate(); err != nil {
		return nil, err
	}
	next.NormalizePwdLevel()

	newVer, err := s.repo.Save(ctx, &next)
	if err != nil {
		return nil, err
	}
	if iErr := s.provider.Invalidate(ctx, t); iErr != nil { // 失效失败 → TTL 兜底，不阻断返回（§8）
		log.Audit("loginconfig.invalidate_failed", map[string]interface{}{"type": int(t), "err": iErr.Error()})
	}
	s.auditDiff(ctx, t, cur, &next, opUID, p)
	return &UpdateResp{Version: newVer}, nil
}

// Reset 恢复该类型默认值（§3.2 seed 值；前端须二次确认）。
// 行缺失 → 用固定 seed ID 建行；存在 → 乐观锁覆盖（读到旧 version 参与竞争）。
func (s *Service) Reset(ctx context.Context, t dloginconfig.ConfigType, opUID int64, p types.RequestParams) (*UpdateResp, error) {
	if !t.Valid() {
		return nil, dloginconfig.ErrNotFound
	}
	cur, err := s.repo.ByType(ctx, t)
	if err != nil {
		return nil, err
	}
	def := dloginconfig.Defaults(t)
	def.NormalizePwdLevel()
	def.UpdatedBy = opUID
	if cur == nil {
		def.ID = t.SeedID()
		if err := s.repo.Create(ctx, def); err != nil {
			return nil, err
		}
		if iErr := s.provider.Invalidate(ctx, t); iErr != nil {
			log.Audit("loginconfig.invalidate_failed", map[string]interface{}{"type": int(t), "err": iErr.Error()})
		}
		s.auditDiff(ctx, t, nil, def, opUID, p)
		return &UpdateResp{Version: def.Version + 1}, nil
	}
	def.ID = cur.ID
	def.Version = cur.Version
	newVer, err := s.repo.Save(ctx, def)
	if err != nil {
		return nil, err
	}
	if iErr := s.provider.Invalidate(ctx, t); iErr != nil {
		log.Audit("loginconfig.invalidate_failed", map[string]interface{}{"type": int(t), "err": iErr.Error()})
	}
	s.auditDiff(ctx, t, cur, def, opUID, p)
	return &UpdateResp{Version: newVer}, nil
}

// ---------- 内部 ----------

// view 单类型视图（行缺失 → 兜底默认 + fallback=true）。
func (s *Service) view(ctx context.Context, t dloginconfig.ConfigType) (*ConfigView, error) {
	cfg, err := s.repo.ByType(ctx, t)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &ConfigView{Config: dloginconfig.Defaults(t), Fallback: true}, nil
	}
	return &ConfigView{Config: cfg}, nil
}

// applyReq 请求体 → 聚合（布尔三态：nil=不改；其余全量覆盖）。
func applyReq(c *dloginconfig.Config, req *UpdateReq) {
	c.LoginMask = req.LoginMask
	c.FailThreshold = req.FailThreshold
	c.LockMinutes = req.LockMinutes
	c.UnlockMode = req.UnlockMode
	c.CaptchaAfterFails = req.CaptchaAfterFails
	c.IPFailLimit = req.IPFailLimit
	c.IPRateLimit = req.IPRateLimit
	c.AccessTokenTTL = req.AccessTokenTTL
	c.RefreshTokenTTL = req.RefreshTokenTTL
	c.SessionHardTTL = req.SessionHardTTL
	c.SessionMaxConcurrent = req.SessionMaxConcurrent
	if req.KickOnPwdChange != nil {
		c.KickOnPwdChange = *req.KickOnPwdChange
	}
	c.RememberDays = req.RememberDays
	c.PwdLevel = req.PwdLevel
	c.PwdMinLength = req.PwdMinLength
	c.PwdMinClasses = req.PwdMinClasses
	if req.PwdForbidUsername != nil {
		c.PwdForbidUsername = *req.PwdForbidUsername
	}
	c.PwdMaxAgeDays = req.PwdMaxAgeDays
	if req.PwdExpireAction != 0 {
		c.PwdExpireAction = req.PwdExpireAction
	}
	c.PwdRemindDays = req.PwdRemindDays
	c.PwdHistoryCount = req.PwdHistoryCount
	if req.PwdForceFirstChange != nil {
		c.PwdForceFirstChange = *req.PwdForceFirstChange
	}
	if req.AllowSelfChangePwd != nil {
		c.AllowSelfChangePwd = *req.AllowSelfChangePwd
	}
}

// auditDiff 修改前后 diff 审计（§5 约定 3：谁改的 + 改了什么）。
// sys_oper_log 模块未落地（docs 操作日志设计），沿用 log.Audit 结构化打点——
// 登记于登录配置设计 §11 偏差。
func (s *Service) auditDiff(ctx context.Context, t dloginconfig.ConfigType, before, after *dloginconfig.Config, opUID int64, p types.RequestParams) {
	diff := map[string]interface{}{}
	bb, _ := json.Marshal(before)
	ab, _ := json.Marshal(after)
	var bm, am map[string]interface{}
	_ = json.Unmarshal(bb, &bm)
	_ = json.Unmarshal(ab, &am)
	if before == nil {
		diff["created_defaults"] = true
	}
	for k, av := range am {
		if before == nil {
			continue // 建行场景全量即新值，逐键对比无意义
		}
		bv, ok := bm[k]
		if !ok || fmt.Sprintf("%v", bv) != fmt.Sprintf("%v", av) {
			diff[k] = map[string]interface{}{"from": bv, "to": av}
		}
	}
	props := map[string]interface{}{
		"operator_id": opUID, "operator_ip": p.IP, "config_type": int(t),
	}
	if len(diff) > 0 {
		props["diff"] = diff
	}
	log.Audit("loginconfig.update", props)
}
