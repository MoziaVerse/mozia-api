package relay

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/cool"
	"github.com/QuantumNous/new-api/relay/channel/modelrouter"
	"github.com/QuantumNous/new-api/relay/channel/mulerun"
	"github.com/QuantumNous/new-api/relay/channel/seedance"
	taskmodelrouter "github.com/QuantumNous/new-api/relay/channel/task/modelrouter"
	taskmulerun "github.com/QuantumNous/new-api/relay/channel/task/mulerun"
)

// Mozia 私有渠道的 adaptor 路由表。
//
// 通过 GetMoziaAdaptor / GetMoziaTaskAdaptor + 在 relay_adaptor.go 顶部加一行
// hook，实现非侵入式扩展。新增渠道时只往本文件追加 case。

func GetMoziaAdaptor(apiType int) channel.Adaptor {
	switch apiType {
	case common.APITypeMoziaModelRouter:
		return &modelrouter.Adaptor{}
	case common.APITypeMoziaMulerun:
		return &mulerun.Adaptor{}
	}
	return nil
}

func GetMoziaTaskAdaptor(channelType int) channel.TaskAdaptor {
	switch channelType {
	case constant.ChannelTypeMoziaModelRouter:
		return &taskmodelrouter.TaskAdaptor{}
	case constant.ChannelTypeMoziaCool:
		return &cool.TaskAdaptor{}
	case constant.ChannelTypeMoziaSeedance:
		return &seedance.TaskAdaptor{}
	case constant.ChannelTypeMoziaMulerun:
		// mulerun studio 多模态走 OpenAI Sora 标准 video task 接口
		// （/v1/video/generations + GET /v1/video/generations/:task_id），
		// 不暴露 vendor。chat 模型仍走 chat adaptor（同 ChannelType，由
		// new-api 路由层按 URL 自动分派）。
		return &taskmulerun.TaskAdaptor{}
	}
	return nil
}
