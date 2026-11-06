// 账号管理用例（docs §9“账号管理”段、§8.3 超管保护）。
package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"dugu-hr-saas-admin/domain/authn"
	"dugu-hr-saas-admin/domain/sysuser"
	"dugu-hr-saas-admin/pkg/utils/log"
)

// SysUserService 账号管理用例容器。
type SysUserService struct {
	users    sysuser.Repository
	sessions authn.SessionStore
	codes    authn.SmsCodeStore
	lock     authn.LockGuard
	mobile   sysuser.MobileCrypto
	hasher   sysuser.PasswordHasher
	ids      authn.IDGenerator
}

// NewSysUserService 构造。
func NewSysUserService(
	users sysuser.Repository,
	sessions authn.SessionStore,
	codes authn.SmsCodeStore,
	lock authn.LockGuard,
	mobile sysuser.MobileCrypto,
	hasher sysuser.PasswordHasher,
	ids authn.IDGenerator,
) *SysUserService {
	return &SysUserService{
		users: users, sessions: sessions, codes: codes, lock: lock,
		mobile: mobile, hasher: hasher, ids: ids,
	}
}

// List 账号列表（手机只回 mask，永不解密——docs §3.3①“展示走脱敏”）。
func (s *SysUserService) List(ctx context.Context, req *SysUserListReq) (*SysUserListResp, error) {
	q := sysuser.ListQuery{Page: req.Page, PageSize: req.PageSize, Keyword: req.Keyword, Status: req.Status}
	res, err := s.users.List(ctx, q)
	if err != nil {
		return nil, err
	}
	items := make([]UserView, 0, len(res.Items))
	for i := range res.Items {
		items = append(items, toUserView(&res.Items[i]))
	}
	return &SysUserListResp{Items: items, Total: res.Total, Page: res.Page, PageSize: res.PageSize}, nil
}

// Create 创建账号（docs §3.3：username 禁 11 位纯数字；uk_mobile 一人一手机）。
func (s *SysUserService) Create(ctx context.Context, req *CreateSysUserReq, opUID int64, p RequestParams) (*UserView, error) {
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
	u := &sysuser.User{
		ID:         s.ids.New(),
		Username:   req.Username,
		Name:       req.Name,
		Email:      req.Email,
		Status:     sysuser.StatusActive,
		Credential: c,
		Mobile:     m,
	}
	u.PwdUpdatedAt = &now

	// 并发撞唯一键：仓储内转换为 ErrUsernameTaken/ErrMobileTaken（docs §3.1）
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}

	s.audit("sysuser.create", opUID, p, map[string]interface{}{
		"target_id": u.ID, "target_username": u.Username, "target_mobile": u.Mobile.Mask,
	})
	v := toUserView(u)
	return &v, nil
}

// Update 修改账号（name/email/mobile/status；username 不可改）。
func (s *SysUserService) Update(ctx context.Context, targetID int64, req *UpdateSysUserReq, opUID int64, p RequestParams, now time.Time) (*UserView, error) {
	u, err := s.load(ctx, targetID)
	if err != nil {
		return nil, err
	}

	// 超管保护：修改超管资料仅超管本人（docs §8.3）
	if err := s.protectSuper(ctx, u, opUID); err != nil {
		return nil, err
	}
	// 停用超管在任何情况下都禁止（sym 用意：防“通道故障导致平台失管”的反面对称）
	if req.Status != nil && *req.Status != sysuser.StatusActive && u.IsSuper() {
		return nil, errSuperAdminProtected
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
	v := toUserView(updated)
	return &v, nil
}

// Delete 停用（软删，docs §9）+ 全端秒踢。
func (s *SysUserService) Delete(ctx context.Context, targetID int64, opUID int64, p RequestParams, now time.Time) error {
	u, err := s.load(ctx, targetID)
	if err != nil {
		return err
	}
	// 超管保护：不可停用/删除（docs §8.3）
	if u.IsSuper() {
		return errSuperAdminProtected
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

// Unlock 解锁：清 DB 失败计数/冻结 + 清 Redis 计数与锁标记。
func (s *SysUserService) Unlock(ctx context.Context, targetID int64, opUID int64, p RequestParams) error {
	u, err := s.load(ctx, targetID)
	if err != nil {
		return err
	}
	if err := s.users.Unlock(ctx, u.ID); err != nil {
		return err
	}
	if err := s.lock.ResetFail(ctx, u.ID, sysuser.MaskIdentifier(u.Username), ""); err != nil {
		slog.Warn("清除 Redis 锁定标记失败", "user_id", u.ID, "err", err)
	}
	s.audit("sysuser.unlock", opUID, p, map[string]interface{}{"target_id": u.ID})
	return nil
}

// ResetPassword 重置密码（docs §9：强审计）。改密即踢全部会话（docs §7/§10）。
func (s *SysUserService) ResetPassword(ctx context.Context, targetID int64, req *ResetPasswordReq, opUID int64, p RequestParams, now time.Time) error {
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

// errSuperAdminProtected 超管受保护的应用层哨兵（映射 code.ErrorSuperAdminProtected）。
var errSuperAdminProtected = fmt.Errorf("super admin protected")

// SuperAdminProtected 暴露给 errors.go 的哨兵（跨文件同包使用）。
func IsSuperAdminProtected(err error) bool { return err == errSuperAdminProtected }

// load 目标聚合必须存在。
func (s *SysUserService) load(ctx context.Context, id int64) (*sysuser.User, error) {
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
func (s *SysUserService) protectSuper(ctx context.Context, target *sysuser.User, opUID int64) error {
	if !target.IsSuper() {
		return nil
	}
	op, err := s.users.ByID(ctx, opUID)
	if err != nil {
		return err
	}
	if op == nil || op.ID != target.ID {
		return errSuperAdminProtected
	}
	return nil
}

// validUsername 领域不变式包装（形似手机号/11 位纯数字/长度）。
func validUsername(u string) bool {
	if u == "" || len(u) > 64 {
		return false
	}
	if sysuser.LooksLikeMobile(u) { // 含“138-0000-0000”式变体：源头禁止歧义（docs §3.3③）
		return false
	}
	return true
}

// audit 安全审计（docs §8.3：对超管账号的一切操作强制审计；
// 这里对全量管理操作统一审计，超管目标自然被覆盖）。
func (s *SysUserService) audit(action string, opUID int64, p RequestParams, detail map[string]interface{}) {
	props := map[string]interface{}{"operator_id": opUID, "operator_ip": p.IP}
	for k, v := range detail {
		props[k] = v
	}
	log.Audit(action, props)
}
