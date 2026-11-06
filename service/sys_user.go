// service/sys_user.go —— 账号管理 CRUD（docs §9“账号管理”段、§8.3 超管保护）。
package service

import (
	"context"
	"log/slog"
	"time"

	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/consts"
	"dugu-hr-saas-admin/pkg/utils/encryption"
	"dugu-hr-saas-admin/pkg/utils/log"
	"dugu-hr-saas-admin/pkg/utils/uid"
	"dugu-hr-saas-admin/repository/cache"
	"dugu-hr-saas-admin/repository/db/dao"
	"dugu-hr-saas-admin/repository/db/model"
	"dugu-hr-saas-admin/types"
)

// SysUserSrv 账号管理业务。
type SysUserSrv struct{}

// GetSysUserSrv 单例（gin-mall 风格）。
func GetSysUserSrv() *SysUserSrv { return &SysUserSrv{} }

// List 账号列表（手机只回 mask，永不解密——docs §3.3①“展示走脱敏”）。
func (s *SysUserSrv) List(ctx context.Context, req *types.SysUserListReq) (*types.SysUserListResp, error) {
	res, err := dao.NewSysUserDao(ctx).List(dao.SysUserListQuery{
		Page:     req.Page,
		PageSize: req.PageSize,
		Keyword:  req.Keyword,
		Status:   req.Status,
	})
	if err != nil {
		return nil, err
	}
	items := make([]types.UserView, 0, len(res.Items))
	for i := range res.Items {
		items = append(items, *toUserView(&res.Items[i]))
	}
	return &types.SysUserListResp{
		Items:    items,
		Total:    res.Total,
		Page:     res.Page,
		PageSize: res.PageSize,
	}, nil
}

// Create 创建账号（docs §3.3：username 禁 11 位纯数字；uk_mobile 保证一人一手机）。
func (s *SysUserSrv) Create(ctx context.Context, req *types.CreateSysUserReq, opUID int64, opIP string) (*types.UserView, error) {
	if !encryption.ValidUsername(req.Username) || encryption.LooksLikeMobile(req.Username) {
		return nil, ErrBadIdentifierFormat
	}
	if !encryption.StrongPassword(req.Password) {
		return nil, ErrWeakPassword
	}
	mobile, err := encryption.NormalizeMobile(req.Mobile)
	if err != nil {
		return nil, ErrBadIdentifierFormat
	}

	secret := conf.Config.EncryptSecret
	mHash := encryption.MobileHMAC(mobile, secret.MobileHmac)
	mEnc, err := encryption.MobileEncrypt(mobile, secret.MobileAes)
	if err != nil {
		return nil, err
	}
	hash, err := encryption.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	ud := dao.NewSysUserDao(ctx)
	// 预检查冲突（并发由唯一键兜底：uk_username / uk_mobile）
	if exist, err := ud.GetByUsername(req.Username); err != nil {
		return nil, err
	} else if exist != nil {
		return nil, dao.ErrUserNameTaken
	}
	if exist, err := ud.GetByMobileHash(mHash); err != nil {
		return nil, err
	} else if exist != nil {
		return nil, dao.ErrMobileTaken
	}

	now := time.Now()
	u := &model.SysUser{
		Id:           uid.New(),
		Username:     req.Username,
		PasswordHash: hash,
		PasswordAlgo: encryption.AlgoBcrypt,
		Name:         req.Name,
		MobileHash:   mHash,
		MobileEnc:    mEnc,
		MobileMask:   encryption.MaskMobile(mobile),
		Email:        req.Email,
		Status:       model.UserStatusActive,
		PwdUpdatedAt: &now,
	}
	if err := ud.Create(u); err != nil {
		return nil, err
	}

	s.audit("sysuser.create", opUID, opIP, map[string]interface{}{
		"target_id":       u.Id,
		"target_username": u.Username,
		"target_mobile":   u.MobileMask,
	})
	return toUserView(u), nil
}

// Update 修改账号（name/email/mobile/status；username 不可改）。
func (s *SysUserSrv) Update(ctx context.Context, targetID int64, req *types.UpdateSysUserReq, opUID int64, opIP string) (*types.UserView, error) {
	ud := dao.NewSysUserDao(ctx)
	target, err := ud.GetByID(targetID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, dao.ErrNotFound
	}

	// 超管保护：修改超管资料仅超管本人（其余一律拒绝）
	if err := s.protectSuper(ctx, target, opUID); err != nil {
		return nil, err
	}

	// 停用超管在任何情况下都禁止（防“短信通道故障导致平台失管”的对称风险）
	if req.Status != nil && *req.Status != model.UserStatusActive && isSuperUser(target) {
		return nil, ErrSuperAdminProtected
	}

	if emErr := ud.UpdateBasic(target.Id, req.Name, req.Email, req.Status); emErr != nil {
		return nil, emErr
	}

	// 换绑手机：一列拆三列，必须三列一起改（docs §3.3①）
	if req.Mobile != "" {
		mobile, mErr := encryption.NormalizeMobile(req.Mobile)
		if mErr != nil {
			return nil, ErrBadIdentifierFormat
		}
		mHash := encryption.MobileHMAC(mobile, conf.Config.EncryptSecret.MobileHmac)
		mEnc, eErr := encryption.MobileEncrypt(mobile, conf.Config.EncryptSecret.MobileAes)
		if eErr != nil {
			return nil, eErr
		}
		if err := ud.UpdateMobile(target.Id, mHash, mEnc, encryption.MaskMobile(mobile)); err != nil {
			return nil, err
		}
	}

	// 停用 → 全端秒踢（docs §7/§10）
	if req.Status != nil && *req.Status == model.UserStatusDisabled {
		if err := cache.KickUserSessions(target.Id); err != nil {
			slog.Warn("停用账号踢会话失败", "user_id", target.Id, "err", err)
		}
	}

	updated, err := ud.GetByID(target.Id)
	if err != nil {
		return nil, err
	}

	s.audit("sysuser.update", opUID, opIP, map[string]interface{}{
		"target_id":  target.Id,
		"set_status": req.Status,
		"set_mobile": req.Mobile != "",
	})
	return toUserView(updated), nil
}

// Delete 停用（软删，docs §9：DELETE = 停用并保留审计数据）+ 全端秒踢。
func (s *SysUserSrv) Delete(ctx context.Context, targetID int64, opUID int64, opIP string) error {
	ud := dao.NewSysUserDao(ctx)
	target, err := ud.GetByID(targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return dao.ErrNotFound
	}
	// 超管保护：不可删除（docs §8.3）
	if isSuperUser(target) {
		return ErrSuperAdminProtected
	}
	if err := ud.SoftDelete(target.Id, time.Now()); err != nil {
		return err
	}
	if err := cache.KickUserSessions(target.Id); err != nil {
		slog.Warn("停用账号踢会话失败", "user_id", target.Id, "err", err)
	}

	s.audit("sysuser.delete", opUID, opIP, map[string]interface{}{
		"target_id":       target.Id,
		"target_username": target.Username,
	})
	return nil
}

// Unlock 解锁：清 DB 失败计数/冻结 + 清 Redis 计数与锁标记。
func (s *SysUserSrv) Unlock(ctx context.Context, targetID int64, opUID int64, opIP string) error {
	ud := dao.NewSysUserDao(ctx)
	target, err := ud.GetByID(targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return dao.ErrNotFound
	}
	if err := ud.Unlock(target.Id); err != nil {
		return err
	}
	if err := cache.ClearFailLock(target.Id); err != nil {
		slog.Warn("清除 Redis 锁定标记失败", "user_id", target.Id, "err", err)
	}

	s.audit("sysuser.unlock", opUID, opIP, map[string]interface{}{"target_id": target.Id})
	return nil
}

// ResetPassword 重置密码（docs §9：强审计）。改密即踢全部会话（docs §7/§10）。
func (s *SysUserSrv) ResetPassword(ctx context.Context, targetID int64, req *types.ResetPasswordReq, opUID int64, opIP string) error {
	if !encryption.StrongPassword(req.Password) {
		return ErrWeakPassword
	}
	ud := dao.NewSysUserDao(ctx)
	target, err := ud.GetByID(targetID)
	if err != nil {
		return err
	}
	if target == nil {
		return dao.ErrNotFound
	}
	// 超管保护：重置超管密码仅超管本人可执行
	if err := s.protectSuper(ctx, target, opUID); err != nil {
		return err
	}

	hash, err := encryption.HashPassword(req.Password)
	if err != nil {
		return err
	}
	if err := ud.UpdatePassword(target.Id, hash, encryption.AlgoBcrypt, "", time.Now()); err != nil {
		return err
	}
	if err := cache.KickUserSessions(target.Id); err != nil {
		slog.Warn("重置密码踢会话失败", "user_id", target.Id, "err", err)
	}

	s.audit("sysuser.reset_password", opUID, opIP, map[string]interface{}{
		"target_id":       target.Id,
		"target_username": target.Username,
	})
	return nil
}

// ---------- 内部 ----------

// isSuperUser 以固定 username 识别内置超管（docs §8.3）。
func isSuperUser(u *model.SysUser) bool {
	return u != nil && u.Username == consts.SuperAdminUsername
}

// protectSuper 修改/重置超管资料仅超管本人；其余操作者一律拒绝。
func (s *SysUserSrv) protectSuper(ctx context.Context, target *model.SysUser, opUID int64) error {
	if !isSuperUser(target) {
		return nil
	}
	op, err := dao.NewSysUserDao(ctx).GetByID(opUID)
	if err != nil {
		return err
	}
	if op == nil || op.Id != target.Id {
		return ErrSuperAdminProtected
	}
	return nil
}

// audit 安全审计（docs §8.3：对超管账号的一切操作强制审计；
// 这里对全量管理操作统一审计，超管目标自然被覆盖）。
func (s *SysUserSrv) audit(action string, opUID int64, opIP string, detail map[string]interface{}) {
	props := map[string]interface{}{
		"operator_id": opUID,
		"operator_ip": opIP,
	}
	for k, v := range detail {
		props[k] = v
	}
	log.Audit(action, props)
}
