package helper

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"

	"github.com/gin-gonic/gin"
)

// TaskBillingEvaluation evaluates both multiplicative ratios and additive
// surcharges against the effective request body.
func TaskBillingEvaluation(c *gin.Context, model string) (taskbilling.Evaluation, taskbilling.Config, bool, error) {
	config, configured := billing_setting.GetTaskBilling(model)
	if !configured {
		return taskbilling.Evaluation{}, taskbilling.Config{}, false, nil
	}
	if err := taskbilling.Validate(config); err != nil {
		return taskbilling.Evaluation{}, config, true, err
	}
	if config.Mode == taskbilling.ModePerRequest && config.Surcharge == nil {
		return taskbilling.Evaluation{}, config, true, nil
	}
	if c == nil || c.Request == nil {
		return taskbilling.Evaluation{}, config, true, fmt.Errorf("task billing requires a request context")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return taskbilling.Evaluation{}, config, true, fmt.Errorf("read task billing request body: %w", err)
	}
	body, err := storage.Bytes()
	if err != nil {
		return taskbilling.Evaluation{}, config, true, fmt.Errorf("read task billing request body: %w", err)
	}
	var evaluation taskbilling.Evaluation
	if config.Mode == taskbilling.ModeTokenParametric {
		evaluation, err = taskbilling.EvaluateTokenPricing(config, body, relaycommon.TaskRequestHasReferenceVideo(c))
	} else {
		evaluation, err = taskbilling.EvaluatePricing(config, body)
	}
	if err != nil {
		return taskbilling.Evaluation{}, config, true, err
	}
	return evaluation, config, true, nil
}
