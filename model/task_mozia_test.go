package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
)

func TestInitTaskPersistsGlobalaiopcSubmissionKeyForPolling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeMoziaGlobalaiopc,
			ApiKey:      "selected-key",
		},
	}

	task := InitTask(constant.TaskPlatform("200"), info)

	assert.Equal(t, "selected-key", task.PrivateData.Key)
}
