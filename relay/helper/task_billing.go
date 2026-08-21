package helper

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/billing_setting"

	"github.com/gin-gonic/gin"
)

// TaskBillingRatios evaluates a model's explicit task billing configuration
// against the request body after channel parameter overrides. The bool is true
// only when a configuration exists; callers must retain legacy adaptor billing
// when it is false.
func TaskBillingRatios(c *gin.Context, model string) (map[string]float64, taskbilling.Config, bool, error) {
	config, configured := billing_setting.GetTaskBilling(model)
	if !configured {
		return nil, taskbilling.Config{}, false, nil
	}
	if err := taskbilling.Validate(config); err != nil {
		return nil, config, true, err
	}
	if config.Mode == taskbilling.ModePerRequest {
		return nil, config, true, nil
	}
	if c == nil || c.Request == nil {
		return nil, config, true, fmt.Errorf("task billing requires a request context")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, config, true, fmt.Errorf("read task billing request body: %w", err)
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, config, true, fmt.Errorf("read task billing request body: %w", err)
	}
	ratios, err := taskbilling.Evaluate(config, body)
	if err != nil {
		return nil, config, true, err
	}
	return ratios, config, true, nil
}
