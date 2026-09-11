package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type supplierReconciliationTask struct{}

func (supplierReconciliationTask) Type() string            { return "supplier_reconciliation" }
func (supplierReconciliationTask) Interval() time.Duration { return time.Hour }
func (supplierReconciliationTask) NewPayload() any         { return struct{}{} }
func (supplierReconciliationTask) Enabled() bool {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return common.OptionMap[model.SupplierRoutingOptionKey] != ""
}

func (supplierReconciliationTask) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	result := model.DB.WithContext(ctx).Model(&model.SupplierAttempt{}).Where("status = ? AND capacity_until < ?", "pending", time.Now().UnixMilli()).Update("status", "unknown")
	status := model.SystemTaskStatusSucceeded
	message := ""
	if result.Error != nil {
		status = model.SystemTaskStatusFailed
		message = result.Error.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, map[string]any{"pending_reconciliation": result.RowsAffected}, message); err != nil {
		common.SysError("supplier reconciliation task: " + err.Error())
	}
}

func init() { RegisterSystemTaskHandler(supplierReconciliationTask{}) }
