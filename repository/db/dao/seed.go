// 初始超管账号 seed（docs §8.3 与 §11 P0 验收标准：“一个初始超管账号能登录”）。
// 仅在 sys_user 表为空时创建一次，之后永不覆盖。
//
// 注意：所有语句必须走 NewDBClient 派生会话，禁止直接在根连接 _db 上
// 调用 Create/First 等 finisher——根连接的 Statement 是共享可变状态，
// 一次污染会让后续任意模型的克隆链错串 schema。
package dao

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	conf "dugu-hr-saas-admin/config"
	"dugu-hr-saas-admin/consts"
	"dugu-hr-saas-admin/pkg/utils/encryption"
	"dugu-hr-saas-admin/pkg/utils/uid"
	"dugu-hr-saas-admin/repository/db/model"
)

func seedSuperAdmin() error {
	db := NewDBClient(context.Background())

	var count int64
	if err := db.Model(&model.SysUser{}).Count(&count).Error; err != nil {
		return fmt.Errorf("统计 sys_user: %w", err)
	}
	if count > 0 {
		return nil
	}

	b := conf.Config.BootstrapAdmin
	mobile, err := encryption.NormalizeMobile(b.Mobile)
	if err != nil {
		return fmt.Errorf("bootstrapAdmin.mobile 不合法: %w", err)
	}
	if !encryption.StrongPassword(b.Password) {
		return errors.New("bootstrapAdmin.password 不满足密码策略（≥10 位且含两类字符）")
	}
	hash, err := encryption.HashPassword(b.Password)
	if err != nil {
		return err
	}
	enc, err := encryption.MobileEncrypt(mobile, conf.Config.EncryptSecret.MobileAes)
	if err != nil {
		return err
	}

	now := time.Now()
	u := &model.SysUser{
		Id:           uid.New(),
		Username:     consts.SuperAdminUsername,
		PasswordHash: hash,
		PasswordAlgo: encryption.AlgoBcrypt,
		Name:         b.Name,
		MobileHash:   encryption.MobileHMAC(mobile, conf.Config.EncryptSecret.MobileHmac),
		MobileEnc:    enc,
		MobileMask:   encryption.MaskMobile(mobile),
		Email:        b.Email,
		Status:       model.UserStatusActive,
		PwdUpdatedAt: &now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.Create(u).Error; err != nil {
		return fmt.Errorf("创建初始超管: %w", err)
	}

	slog.Warn("[bootstrap] 已创建初始超管账号 username=admin（密码见 bootstrapAdmin/SAAS_BOOTSTRAP_ADMIN_PASSWORD 配置）手机=" + u.MobileMask + "，请登录后尽快修改密码")
	return nil
}
