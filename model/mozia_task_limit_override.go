package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// MoziaUserTaskLimitOverride 是用户级的异步任务限制覆盖，优先于分组配置。
// 用同一张表表达运营的四种处置：
//   - 降速：给更小的 running / queued（或 per_minute 等），带 until
//   - 冷却：running = queued = 0，带 until
//   - 暂停：running = queued = 0，until = 0（永久，直到人工解除）
//   - 解除：删除该行
//
// 字段为 0 表示沿用分组配置；只有 running 与 queued 同为 0 才表示暂停 / 冷却
// （见 service.CollectTaskLimitUsage / evaluateTaskLimit）。
type MoziaUserTaskLimitOverride struct {
	Id          int     `json:"id"`
	UserId      int     `json:"user_id" gorm:"uniqueIndex"`
	Running     int     `json:"running"`
	Queued      int     `json:"queued"`
	PerMinute   int     `json:"per_minute"`
	DailyYuan   float64 `json:"daily_yuan"`
	FailStreak  int     `json:"fail_streak"`
	Until       int64   `json:"until" gorm:"index"` // unix 秒，0 = 永久
	Reason      string  `json:"reason" gorm:"type:varchar(255)"`
	Operator    string  `json:"operator" gorm:"type:varchar(64)"`
	CreatedTime int64   `json:"created_time"`
	UpdatedTime int64   `json:"updated_time"`
}

func (o *MoziaUserTaskLimitOverride) IsActive(now time.Time) bool {
	return o != nil && (o.Until == 0 || o.Until > now.Unix())
}

// GetActiveTaskLimitOverride 取用户当前生效的覆盖；没有或已过期返回 nil。
func GetActiveTaskLimitOverride(userId int, now time.Time) (*MoziaUserTaskLimitOverride, error) {
	var o MoziaUserTaskLimitOverride
	err := DB.Where("user_id = ?", userId).First(&o).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !o.IsActive(now) {
		return nil, nil
	}
	return &o, nil
}

// UpsertTaskLimitOverride 按 user_id 覆盖写入。
func UpsertTaskLimitOverride(o *MoziaUserTaskLimitOverride) error {
	now := GetDBTimestamp()
	var existing MoziaUserTaskLimitOverride
	err := DB.Where("user_id = ?", o.UserId).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		o.CreatedTime = now
		o.UpdatedTime = now
		return DB.Create(o).Error
	}
	if err != nil {
		return err
	}
	o.Id = existing.Id
	o.CreatedTime = existing.CreatedTime
	o.UpdatedTime = now
	return DB.Model(&existing).Select("*").Updates(o).Error
}

func DeleteTaskLimitOverride(userId int) error {
	return DB.Where("user_id = ?", userId).Delete(&MoziaUserTaskLimitOverride{}).Error
}

// ListTaskLimitOverrides 列出全部覆盖（含已过期，由调用方标注）。
func ListTaskLimitOverrides() ([]MoziaUserTaskLimitOverride, error) {
	var rows []MoziaUserTaskLimitOverride
	err := DB.Order("updated_time DESC").Find(&rows).Error
	return rows, err
}

// GetUserRecentTasks 取用户最近 since 秒内提交的任务加上所有未终结任务，
// 供并发 / 频率 / 日消费 / 连续失败判定使用。按 id 倒序。
func GetUserRecentTasks(userId int, sinceUnix int64, limit int) []*Task {
	var tasks []*Task
	err := DB.Where("user_id = ?", userId).
		Where("submit_time >= ? OR status NOT IN ?", sinceUnix, []string{TaskStatusFailure, TaskStatusSuccess}).
		Order("id DESC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil
	}
	return tasks
}
