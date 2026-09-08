package model

import (
	"time"

	"github.com/QuantumNous/new-api/constant"
)

// Ark exposes task history for seven days. Always establish ownership locally
// before sending task IDs to a shared upstream credential.
func GetVolcengineVideoTasks(userID int, taskIDs []string) ([]*Task, error) {
	var tasks []*Task
	query := DB.Where("user_id = ? AND platform = ? AND submit_time >= ?", userID, constant.TaskPlatformVolcengineVideo, time.Now().Add(-7*24*time.Hour).Unix())
	if len(taskIDs) != 0 {
		query = query.Where("task_id IN ?", taskIDs)
	}
	err := query.Order("id DESC").Find(&tasks).Error
	return tasks, err
}
