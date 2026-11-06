package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"dugu-hr-saas-admin/pkg/utils/uid"
	"dugu-hr-saas-admin/repository/db/model"
)

// SysUserDao sys_user 表仓储。
type SysUserDao struct{ ctx context.Context }

// NewSysUserDao 以请求 ctx 构造仓储（超时/取消随请求传播）。
func NewSysUserDao(ctx context.Context) *SysUserDao { return &SysUserDao{ctx} }

func (d *SysUserDao) db() *gorm.DB { return NewDBClient(d.ctx) }

// GetByID 按主键查询（软删过滤 deleted_at=0），未命中返回 (nil, nil)。
func (d *SysUserDao) GetByID(id int64) (*model.SysUser, error) {
	var po model.SysUser
	err := d.db().First(&po, "id = ? AND deleted_at = 0", id).Error
	if isNotFoundErr(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sys_user.get_by_id: %w", err)
	}
	return &po, nil
}

// GetByUsername 按账号精确匹配（唯一索引，登录解析①），未命中返回 (nil, nil)。
func (d *SysUserDao) GetByUsername(username string) (*model.SysUser, error) {
	var po model.SysUser
	err := d.db().First(&po, "username = ? AND deleted_at = 0", username).Error
	if isNotFoundErr(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sys_user.get_by_username: %w", err)
	}
	return &po, nil
}

// GetByMobileHash 按手机哈希匹配（唯一索引，登录解析②），未命中返回 (nil, nil)。
func (d *SysUserDao) GetByMobileHash(hash string) (*model.SysUser, error) {
	var po model.SysUser
	err := d.db().First(&po, "mobile_hash = ? AND deleted_at = 0", hash).Error
	if isNotFoundErr(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sys_user.get_by_mobile_hash: %w", err)
	}
	return &po, nil
}

// Create 新增账号。唯一键冲突按 uk_username / uk_mobile 归类错误。
func (d *SysUserDao) Create(u *model.SysUser) error {
	if u.Id == 0 {
		u.Id = uid.New()
	}
	now := time.Now()
	u.CreatedAt, u.UpdatedAt = now, now
	if err := d.db().Create(u).Error; err != nil {
		if idxName, dup := isDuplicate(err); dup {
			if idxName == "uk_mobile" {
				return ErrMobileTaken
			}
			return ErrUserNameTaken
		}
		return fmt.Errorf("sys_user.create: %w", err)
	}
	return nil
}

// UpdateBasic 修改 name/email/status（username 不可改：账号是登录解析的锚点）。
// 受保护规则由 service 层判断后调用。
func (d *SysUserDao) UpdateBasic(id int64, name, email string, status *int) error {
	sets := map[string]interface{}{}
	if name != "" {
		sets["name"] = name
	}
	if email != "" {
		sets["email"] = email
	}
	if status != nil {
		sets["status"] = *status
	}
	if len(sets) == 0 {
		return nil
	}
	res := d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(sets)
	if res.Error != nil {
		return fmt.Errorf("sys_user.update_basic: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateMobile 换绑手机号：一列拆三列，必须三列一起改（§3.3①）。
func (d *SysUserDao) UpdateMobile(id int64, hash, enc, mask string) error {
	res := d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"mobile_hash": hash,
			"mobile_enc":  enc,
			"mobile_mask": mask,
		})
	if res.Error != nil {
		if idxName, dup := isDuplicate(res.Error); dup && idxName == "uk_mobile" {
			return ErrMobileTaken
		}
		return fmt.Errorf("sys_user.update_mobile: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePassword 更新密码相关字段（改密/重置/旧算法透明升级共用）。
func (d *SysUserDao) UpdatePassword(id int64, hash, algo, salt string, pwdUpdatedAt time.Time) error {
	res := d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"password_hash":  hash,
			"password_algo":  algo,
			"salt":           salt,
			"pwd_updated_at": pwdUpdatedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("sys_user.update_password: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLoginState 登录成功后：写最近登录、清失败计数与锁定；status 若因锁定变 3 则还原为 1。
func (d *SysUserDao) UpdateLoginState(id int64, ip string, at time.Time) error {
	return d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"last_login_at": at,
			"last_login_ip": ip,
			"fail_count":    0,
			"locked_until":  nil,
			"status":        gorm.Expr("IF(status = 3, 1, status)"),
		}).Error
}

// IncFail 失败计数 DB 双写（Redis 是加速，DB 是兜底，docs §6）。
// threshold 达到即写 locked_until 并置 status=3；锁定期内的尝试不重置计时
// （由调用方保证：锁定时不调本方法）。
// 返回失败后的最新计数与是否触发锁定。
func (d *SysUserDao) IncFail(id int64, threshold int, lock time.Duration, now time.Time) (cnt int, lockTriggered bool, err error) {
	// 单条 UPDATE 原子自增，避免并发读改写
	res := d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Update("fail_count", gorm.Expr("fail_count + 1"))
	if res.Error != nil {
		return 0, false, fmt.Errorf("sys_user.inc_fail: %w", res.Error)
	}

	var po model.SysUser
	if err = d.db().First(&po, "id = ?", id).Error; err != nil {
		return 0, false, fmt.Errorf("sys_user.inc_fail.reload: %w", err)
	}
	if po.FailCount >= threshold {
		lockUntil := now.Add(lock)
		if err = d.db().Model(&model.SysUser{}).
			Where("id = ? AND deleted_at = 0", id).
			Updates(map[string]interface{}{
				"locked_until": lockUntil,
				"status":       model.UserStatusLocked,
			}).Error; err != nil {
			return po.FailCount, false, fmt.Errorf("sys_user.inc_fail.lock: %w", err)
		}
		return po.FailCount, true, nil
	}
	return po.FailCount, false, nil
}

// SetLocked 写锁定（撞库等需要绕过计数的场景）。
func (d *SysUserDao) SetLocked(id int64, lock time.Duration) error {
	now := time.Now()
	return d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"locked_until": now.Add(lock),
			"status":       model.UserStatusLocked,
		}).Error
}

// Unlock 解锁：清失败计数、解冻、还原状态。
func (d *SysUserDao) Unlock(id int64) error {
	res := d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Updates(map[string]interface{}{
			"fail_count":   0,
			"locked_until": nil,
			"status":       model.UserStatusActive,
		})
	if res.Error != nil {
		return fmt.Errorf("sys_user.unlock: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDelete 软删（DELETE 接口语义：停用并保留审计数据）。
// deleted_at 写毫秒戳以参与唯一键（docs §3.1）：同一手机/账号可重新建号。
func (d *SysUserDao) SoftDelete(id int64, at time.Time) error {
	res := d.db().Model(&model.SysUser{}).
		Where("id = ? AND deleted_at = 0", id).
		Update("deleted_at", at.UnixMilli())
	if res.Error != nil {
		return fmt.Errorf("sys_user.soft_delete: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SysUserListQuery 账号列表查询条件。
type SysUserListQuery struct {
	Page     int
	PageSize int
	Keyword  string // 姓名/账号模糊
	Status   *int
}

// SysUserListResult 账号列表结果。
type SysUserListResult struct {
	Items    []model.SysUser
	Total    int64
	Page     int
	PageSize int
}

// List 账号列表（脱敏后由 service 组装响应，手机只见 mask）。
func (d *SysUserDao) List(q SysUserListQuery) (*SysUserListResult, error) {
	page, size := normalizePage(q.Page, q.PageSize)
	db := d.db().Model(&model.SysUser{}).Where("deleted_at = 0")
	if kw := q.Keyword; kw != "" {
		like := "%" + kw + "%"
		db = db.Where("`name` LIKE ? OR username LIKE ?", like, like)
	}
	if q.Status != nil {
		db = db.Where("status = ?", *q.Status)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, fmt.Errorf("sys_user.list.count: %w", err)
	}

	var items []model.SysUser
	if err := db.Order("id DESC").Limit(size).Offset((page - 1) * size).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("sys_user.list.find: %w", err)
	}
	return &SysUserListResult{Items: items, Total: total, Page: page, PageSize: size}, nil
}

// normalizePage 兜底修正分页参数。
func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// GetLoginStates 登录态兜底查询（Redis 故障时用 DB：docs §6）。
func (d *SysUserDao) GetLoginState(id int64) (failCount int, lockedUntil *time.Time, err error) {
	var po model.SysUser
	if err = d.db().Select("fail_count", "locked_until", "status").
		First(&po, "id = ? AND deleted_at = 0", id).Error; err != nil {
		return 0, nil, fmt.Errorf("sys_user.get_login_state: %w", err)
	}
	return po.FailCount, po.LockedUntil, nil
}

// isNotFoundErr gorm 未找到错误归一化。
func isNotFoundErr(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
