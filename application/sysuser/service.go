// Package sysuser —— 账号管理用例（docs §9“账号管理”段、§8.3 超管保护）。
// 标准DDD：依赖只有 domain（端口+聚合）；DTO 在本包，与用例同域同包。
package sysuser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"dugu-hr-saas-admin/application/types"
	authn "dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/sysuser"
	"dugu-hr-saas-admin/pkg/code"
	dlog "dugu-hr-saas-admin/pkg/utils/log"
)

// ---------- 用例输出/输入对象（接口层直接绑定） ----------

// UserView 对外展示视图（手机只用 mask，永不解密——docs §3.3①）。
// 语言中立约定：
//   - id 用字符串输出——雪花 int64 超过 JS Number.MAX_SAFE_INTEGER（2^53），
//     Java 端也存在各 JSON 库对大 long 的缺省行为差异，统一字符串最稳；
//   - 时间为 "yyyy-MM-dd HH:mm:ss" 字符串（我们自己格式化，不经手 Go time.Time）。
type UserView struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Mobile    string `json:"mobile"`
	Status    int    `json:"status"`
	CreatedAt string `json:"created_at"`
}

// toView 聚合 → 展示视图（包内私有；auth 域经 ToView 复用）。
func toView(u *sysuser.User) UserView {
	v := UserView{
		ID:       strconv.FormatInt(u.ID, 10),
		Username: u.Username,
		Name:     u.Name,
		Email:    u.Email,
		Mobile:   u.Mobile.Mask,
		Status:   int(u.Status),
	}
	if !u.CreatedAt.IsZero() {
		v.CreatedAt = u.CreatedAt.Format(time.DateTime)
	}
	return v
}

// ToView 导出视图转换（认证域登录响应/Me 复用，避免同层重复定义）。
func ToView(u *sysuser.User) UserView { return toView(u) }

// CreateSysUserReq 创建管理账号（username 禁 11 位纯数字；密码须 ≥10 位含两类字符）。
type CreateSysUserReq struct {
	Username string `json:"username" binding:"required,max=64"`
	Name     string `json:"name" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=64"`
	Mobile   string `json:"mobile" binding:"required"`
	Email    string `json:"email" binding:"omitempty,email,max=128"`
}

// UpdateSysUserReq 修改账号（username 不可改；status 仅 1/2；mobile 传入即换绑）。
type UpdateSysUserReq struct {
	Name   string          `json:"name" binding:"omitempty,max=64"`
	Email  string          `json:"email" binding:"omitempty,email,max=128"`
	Mobile string          `json:"mobile" binding:"omitempty"`
	Status *sysuser.Status `json:"status" binding:"omitempty,oneof=1 2"`
}

// SysUserListReq 账号列表。
type SysUserListReq struct {
	Page     int             `form:"page" binding:"omitempty,min=1"`
	PageSize int             `form:"page_size" binding:"omitempty,min=1,max=100"`
	Keyword  string          `form:"keyword"`
	Status   *sysuser.Status `form:"status" binding:"omitempty,oneof=1 2 3"`
}

// SysUserListResp 账号列表响应。
type SysUserListResp struct {
	Items    []UserView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// ResetPasswordReq 重置密码（docs §9：强审计）。
type ResetPasswordReq struct {
	Password string `json:"password" binding:"required,max=64"`
}

// ---------- 批量冻结/解冻（单条接口由批量接口替代） ----------

// Freezer 时长常量：人工冻结默认 60 分钟、可配 1~10080 分钟。
// docs 的 15 分钟是“连错 5 次”自动锁定的语义，二者不混用。
const (
	DefaultFreezeMinutes = 60
	MaxFreezeMinutes     = 10080
)

// BatchFreezeReq 批量冻结（ids 一律字符串——响应合同的 ID 约定）。
type BatchFreezeReq struct {
	IDs     []string `json:"ids" binding:"required,min=1,max=100"`
	Minutes int      `json:"minutes" binding:"omitempty,min=1"`
}

// BatchUnlockReq 批量解冻。
type BatchUnlockReq struct {
	IDs []string `json:"ids" binding:"required,min=1,max=100"`
}

// BatchItemResult 逐项结果：独立处理，部分失败不影响已成功项。
type BatchItemResult struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Code    int    `json:"code,omitempty"` // 失败时为该条业务码（pkg/code）
}

// BatchResp 批量响应（succeeded+failed = len(items)）。
type BatchResp struct {
	Items     []BatchItemResult `json:"items"`
	Succeeded int               `json:"succeeded"`
	Failed    int               `json:"failed"`
}

// ---------- 业务哨兵 ----------

// ErrSuperAdminProtected 超管账号受保护（docs §8.3，映射 code.ErrorSuperAdminProtected）。
var ErrSuperAdminProtected = fmt.Errorf("super admin protected")

// ErrSelfOperation 禁止对自己账号执行停用/启用、锁定/解锁类操作。
// 语义：这类“生命线”操作（管理员 自干扰防线）须由他人执行/协作。
var ErrSelfOperation = fmt.Errorf("sysuser: cannot operate self")

// ---------- 用例 ----------

// Service 账号管理用例。
type Service struct {
	users    sysuser.Repository
	sessions authn.SessionStore
	lock     authn.LockGuard
	mobile   sysuser.MobileCrypto
	hasher   sysuser.PasswordHasher
	ids      authn.IDGenerator
}

// NewService 构造（认证域 DTOstatus 与本域无耦合，登录会话仅通过 SessionStore 端口协作）。
func NewService(
	users sysuser.Repository,
	sessions authn.SessionStore,
	lock authn.LockGuard,
	mobile sysuser.MobileCrypto,
	hasher sysuser.PasswordHasher,
	ids authn.IDGenerator,
) *Service {
	return &Service{users: users, sessions: sessions, lock: lock, mobile: mobile, hasher: hasher, ids: ids}
}

// List 账号列表（手机只回 mask，永不解密）。
func (s *Service) List(ctx context.Context, req *SysUserListReq) (*SysUserListResp, error) {
	res, err := s.users.List(ctx, sysuser.ListQuery{
		Page: req.Page, PageSize: req.PageSize, Keyword: req.Keyword, Status: req.Status,
	})
	if err != nil {
		return nil, err
	}
	items := make([]UserView, 0, len(res.Items))
	for i := range res.Items {
		items = append(items, toView(&res.Items[i]))
	}
	return &SysUserListResp{Items: items, Total: res.Total, Page: res.Page, PageSize: res.PageSize}, nil
}

// Create 创建账号（docs §3.3：username 禁 11 位纯数字；uk_mobile 一人一手机）。
func (s *Service) Create(ctx context.Context, req *CreateSysUserReq, opUID int64, p types.RequestParams) (*UserView, error) {
	if !validUsername(req.Username) {
		return nil, sysuser.ErrBadUsername
	}
	if err := sysuser.ValidatePassword(req.Password); err != nil {
		return nil, err
	}
	m, err := sysuser.BindMobile(req.Mobile, s.mobile)
	if err != nil {
		return nil, err
	}
	c, err := s.hasher.Hash(req.Password)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	c.PwdUpdatedAt = &now
	u := &sysuser.User{
		ID:         s.ids.New(),
		Username:   req.Username,
		Name:       req.Name,
		Email:      req.Email,
		Status:     sysuser.StatusActive,
		Credential: c,
		Mobile:     m,
	}
	// 并发撞唯一键：仓储内转换为 ErrUsernameTaken/ErrMobileTaken（docs §3.1）
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}

	s.audit("sysuser.create", opUID, p, map[string]interface{}{
		"target_id": u.ID, "target_username": u.Username, "target_mobile": u.Mobile.Mask,
	})
	v := toView(u)
	return &v, nil
}

// Update 修改账号（name/email/mobile/status；username 不可改）。
func (s *Service) Update(ctx context.Context, targetID int64, req *UpdateSysUserReq, opUID int64, p types.RequestParams, now time.Time) (*UserView, error) {
	u, err := s.load(ctx, targetID)
	if err != nil {
		return nil, err
	}
	// 超管保护：修改超管资料仅超管本人（docs §8.3）
	if err := s.protectSuper(ctx, u, opUID); err != nil {
		return nil, err
	}
	// 禁止自我停用/启用（管理员自干扰防线：不能切自己的状态水龙头）
	if req.Status != nil && u.ID == opUID {
		return nil, ErrSelfOperation
	}
	// 停用超管在任何情况下都禁止（防“通道故障导致平台失管”的反面对称）
	if req.Status != nil && *req.Status != sysuser.StatusActive && u.IsSuper() {
		return nil, ErrSuperAdminProtected
	}

	if err := s.users.SaveProfile(ctx, u.ID, req.Name, req.Email, req.Status); err != nil {
		return nil, err
	}
	// 换绑手机：一列拆三列，必须三列一起改（docs §3.3①）
	if req.Mobile != "" {
		m, mErr := sysuser.BindMobile(req.Mobile, s.mobile)
		if mErr != nil {
			return nil, mErr
		}
		if err := s.users.UpdateMobile(ctx, u.ID, m); err != nil {
			return nil, err
		}
	}
	// 停用 → 全端秒踢（docs §7/§10）
	if req.Status != nil && *req.Status == sysuser.StatusDisabled {
		if err := s.sessions.KickAll(ctx, u.ID); err != nil {
			slog.Warn("停用账号踢会话失败", "user_id", u.ID, "err", err)
		}
	}

	updated, err := s.load(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	s.audit("sysuser.update", opUID, p, map[string]interface{}{
		"target_id": u.ID, "set_status": req.Status, "set_mobile": req.Mobile != "",
	})
	v := toView(updated)
	return &v, nil
}

// Delete 停用（软删，docs §9）+ 全端秒踢。
func (s *Service) Delete(ctx context.Context, targetID int64, opUID int64, p types.RequestParams, now time.Time) error {
	u, err := s.load(ctx, targetID)
	if err != nil {
		return err
	}
	// 超管保护：不可停用/删除（docs §8.3）
	if u.IsSuper() {
		return ErrSuperAdminProtected
	}
	// 禁止自我删除（是停用语义；寻找旁路＝放弃本条防线，补一条关键闭环）
	if u.ID == opUID {
		return ErrSelfOperation
	}
	if err := s.users.SoftDelete(ctx, u.ID, now); err != nil {
		return err
	}
	if err := s.sessions.KickAll(ctx, u.ID); err != nil {
		slog.Warn("停用账号踢会话失败", "user_id", u.ID, "err", err)
	}
	s.audit("sysuser.delete", opUID, p, map[string]interface{}{
		"target_id": u.ID, "target_username": u.Username,
	})
	return nil
}

// FreezeUsers 批量冻结：逐项独立（load → 越权守卫 → DB/Redis 双写 → 秒踢全端）；任一项失败不影响其他项。
// 语义：locked_until = now + minutes（缺省 DefaultFreezeMinutes），status 置 3。
func (s *Service) FreezeUsers(ctx context.Context, req *BatchFreezeReq, opUID int64, p types.RequestParams, now time.Time) *BatchResp {
	minutes := DefaultFreezeMinutes
	if req.Minutes > 0 {
		if req.Minutes > MaxFreezeMinutes {
			minutes = MaxFreezeMinutes
		} else {
			minutes = req.Minutes
		}
	}
	until := now.Add(time.Duration(minutes) * time.Minute)

	out := &BatchResp{Items: make([]BatchItemResult, 0, len(req.IDs))}
	for _, idStr := range req.IDs {
		id, perr := strconv.ParseInt(idStr, 10, 64)
		if perr != nil {
			out.add(idStr, false, code.ErrorBadIdentifierFormat)
			continue
		}
		u, err := s.load(ctx, id)
		if err != nil {
			out.add(idStr, false, CodeFor(err))
			continue
		}
		if u.IsSuper() { // 超管保护：不可冻结（docs §8.3 平台失管反面）
			out.add(idStr, false, code.ErrorSuperAdminProtected)
			continue
		}
		if u.ID == opUID { // 冻结/解冻不可自我操作
			out.add(idStr, false, code.ErrorSelfOperation)
			continue
		}
		if err := s.users.MarkLocked(ctx, u.ID, until); err != nil {
			out.add(idStr, false, CodeFor(err))
			continue
		}
		if lerr := s.lock.MarkLocked(ctx, u.ID, time.Until(until)); lerr != nil {
			// Redis 是加速层；DB locked_until 已生效
			slog.Warn("Redis 写锁定标记失败", "user_id", u.ID, "err", lerr)
		}
		if kerr := s.sessions.KickAll(ctx, u.ID); kerr != nil {
			slog.Warn("冻结账号踢会话失败", "user_id", u.ID, "err", kerr)
		}
		out.add(idStr, true, 0)
		s.audit("sysuser.freeze", opUID, p, map[string]interface{}{
			"target_id": u.ID, "target_username": u.Username,
			"minutes": minutes, "until": until.Format(time.DateTime),
		})
	}
	out.tally()
	return out
}

// UnfreezeUsers 批量解冻：清 DB 失败计数/冻结 + 清 Redis 计数与锁标记；逐项独立。
func (s *Service) UnfreezeUsers(ctx context.Context, req *BatchUnlockReq, opUID int64, p types.RequestParams) *BatchResp {
	out := &BatchResp{Items: make([]BatchItemResult, 0, len(req.IDs))}
	for _, idStr := range req.IDs {
		id, perr := strconv.ParseInt(idStr, 10, 64)
		if perr != nil {
			out.add(idStr, false, code.ErrorBadIdentifierFormat)
			continue
		}
		u, err := s.load(ctx, id)
		if err != nil {
			out.add(idStr, false, CodeFor(err))
			continue
		}
		if u.ID == opUID {
			out.add(idStr, false, code.ErrorSelfOperation)
			continue
		}
		if err := s.users.Unlock(ctx, u.ID); err != nil {
			out.add(idStr, false, CodeFor(err))
			continue
		}
		if lerr := s.lock.ResetFail(ctx, u.ID, sysuser.MaskIdentifier(u.Username), ""); lerr != nil {
			slog.Warn("清除 Redis 锁定标记失败", "user_id", u.ID, "err", lerr)
		}
		out.add(idStr, true, 0)
		s.audit("sysuser.unfreeze", opUID, p, map[string]interface{}{
			"target_id": u.ID, "target_username": u.Username,
		})
	}
	out.tally()
	return out
}

// CodeFor 本域错误 → 业务码（批量明细逐项使用；跨域总映射在 application.CodeOf）。
func CodeFor(err error) int {
	switch {
	case errors.Is(err, sysuser.ErrNotFound):
		return code.ErrorSysUserNotFound
	case errors.Is(err, ErrSuperAdminProtected):
		return code.ErrorSuperAdminProtected
	case errors.Is(err, ErrSelfOperation):
		return code.ErrorSelfOperation
	}
	return code.ErrorDatabase
}

func (b *BatchResp) add(id string, ok bool, codeNum int) {
	b.Items = append(b.Items, BatchItemResult{ID: id, Success: ok, Code: codeNum})
}

func (b *BatchResp) tally() {
	for _, it := range b.Items {
		if it.Success {
			b.Succeeded++
		} else {
			b.Failed++
		}
	}
}

// ResetPassword 重置密码（docs §9：强审计）。改密即踢全部会话（docs §7/§10）。
func (s *Service) ResetPassword(ctx context.Context, targetID int64, req *ResetPasswordReq, opUID int64, p types.RequestParams, now time.Time) error {
	if err := sysuser.ValidatePassword(req.Password); err != nil {
		return err
	}
	u, err := s.load(ctx, targetID)
	if err != nil {
		return err
	}
	// 超管保护：重置超管密码仅超管本人
	if err := s.protectSuper(ctx, u, opUID); err != nil {
		return err
	}

	c, err := s.hasher.Hash(req.Password)
	if err != nil {
		return err
	}
	c.PwdUpdatedAt = &now
	if err := s.users.UpdatePassword(ctx, u.ID, c); err != nil {
		return err
	}
	if err := s.sessions.KickAll(ctx, u.ID); err != nil {
		slog.Warn("重置密码踢会话失败", "user_id", u.ID, "err", err)
	}
	s.audit("sysuser.reset_password", opUID, p, map[string]interface{}{
		"target_id": u.ID, "target_username": u.Username,
	})
	return nil
}

// ---------- 内部 ----------

// load 目标聚合必须存在（未命中映射为 code.ErrorSysUserNotFound）。
func (s *Service) load(ctx context.Context, id int64) (*sysuser.User, error) {
	u, err := s.users.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, sysuser.ErrNotFound
	}
	return u, nil
}

// protectSuper 修改/重置超管资料仅超管本人；其余操作者拒绝。
func (s *Service) protectSuper(ctx context.Context, target *sysuser.User, opUID int64) error {
	if !target.IsSuper() {
		return nil
	}
	op, err := s.users.ByID(ctx, opUID)
	if err != nil {
		return err
	}
	if op == nil || op.ID != target.ID {
		return ErrSuperAdminProtected
	}
	return nil
}

// validUsername 领域不变式包装（形似手机号/11 位纯数字/长度，docs §3.3③）。
func validUsername(u string) bool {
	if u == "" || len(u) > 64 {
		return false
	}
	if sysuser.LooksLikeMobile(u) { // 含“138-0000-0000”式变体：源头禁歧义
		return false
	}
	return true
}

// audit 安全审计（docs §8.3：对超管账号的一切操作强制审计；
// 这里对全量管理操作统一审计，超管目标自然被覆盖）。
func (s *Service) audit(action string, opUID int64, p types.RequestParams, detail map[string]interface{}) {
	props := map[string]interface{}{"operator_id": opUID, "operator_ip": p.IP}
	for k, v := range detail {
		props[k] = v
	}
	dlog.Audit(action, props)
}
