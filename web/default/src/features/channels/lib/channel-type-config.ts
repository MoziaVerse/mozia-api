/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { CHANNEL_TYPES } from '../constants'

// ============================================================================
// Channel Type Configuration
// ============================================================================

export interface ChannelTypeConfig {
  id: number
  name: string
  icon: string
  defaultBaseUrl?: string
  requiresOrganization?: boolean
  requiresRegion?: boolean
  supportedModels?: string[]
  hints?: {
    baseUrl?: string
    key?: string
    models?: string
    other?: string
  }
  validation?: {
    keyFormat?: RegExp
    keyMinLength?: number
  }
}

/**
 * Configuration for each channel type
 */
export const CHANNEL_TYPE_CONFIGS: Record<number, ChannelTypeConfig> = {
  // ===== Mozia 私有渠道 =====
  200: {
    id: 200,
    name: CHANNEL_TYPES[200],
    icon: 'openai',
    defaultBaseUrl: 'https://zcbservice.aizfw.cn/kyyReactApiServer',
    hints: {
      baseUrl: 'Default: https://zcbservice.aizfw.cn/kyyReactApiServer',
      key: 'Globalaiopc 控制台分发的 API Key',
      models:
        '模型列表填写对外名称，并在“模型映射”中映射到 Globalaiopc Videos 模型：videos、videos_stable、videos_stable_fast、videos_pro 或 videos_pro_fast',
      other:
        '客户端统一调用 /v1/videos 或 /v1/video/generations；上游提交使用 /v1/videos/videos，查询使用 /v1/result/{id}；支持图片、视频、音频参考素材映射，模型价格按次填写',
    },
  },
  201: {
    id: 201,
    name: CHANNEL_TYPES[201],
    icon: 'openai',
    defaultBaseUrl: 'https://api.mulerun.com',
    hints: {
      baseUrl: 'Default: https://api.mulerun.com',
      key: 'Format: muk-...',
      models:
        'Chat（OpenAI/Anthropic 双协议）：openai/gpt-5.5、google/gemini-3.1-pro-preview、moonshot/kimi-k2.6、zhipu/glm-5.1 ...\n多模态任务（异步）：alibaba/wan2.6-t2v、bytedance/seedance-2.0:image-to-video、google/veo3、klingai/kling-v3-omni-t2v、midjourney/diffusion、minimax/music-2.5、openai/gpt-image-2 ...',
      other:
        '同一渠道同时承载 chat 与 task：/v1/chat/completions 与 /v1/messages 走 chat 适配器，/v1/tasks/* 走 mulerun task 适配器；多动作模型（seedance/gpt-image-2/nano-banana）可在 model 字段后追加 ":<action>" 指定，如 "openai/gpt-image-2:edit"',
    },
    validation: {
      keyFormat: /^muk-/,
      keyMinLength: 20,
    },
  },
  202: {
    id: 202,
    name: CHANNEL_TYPES[202],
    icon: 'openai',
    defaultBaseUrl: 'https://api.mjapi.cc.cd',
    hints: {
      baseUrl: 'Default: https://api.mjapi.cc.cd',
      key: 'Cool API Key（sk- 前缀走自动扣费/退费）',
      models:
        '图片：gpt_image_2, midjourney_v7, flux_kontext_pro ...; 视频：seedance_2_fast, kling_3_omni, vidu_q3_pro ...',
      other:
        'Cool 走异步视频任务入口：提交使用 /v1/video/generations 或 /v1/videos，查询使用 GET /v1/video/generations/{task_id} 或 GET /v1/videos/{task_id}',
    },
  },
  203: {
    id: 203,
    name: CHANNEL_TYPES[203],
    icon: 'openai',
    defaultBaseUrl: '',
    hints: {
      baseUrl:
        '必填：供应商 Base URL，可填写到域名根路径或 /v1，例如 https://pulseaify.com 或 https://pulseaify.com/v1',
      key: '供应商 API Key（使用 Authorization: Bearer）',
      models:
        '填写对外模型名，并在模型映射中指定供应商模型 ID，例如 {"cool:seedance_2_720p":"video-1"}',
      other:
        '兼容 POST/GET /v1/video/generations；duration 必须显式传正整数。模型价格按每秒价格填写，实际费用 = 模型价格 × duration；失败或超时自动退款',
    },
  },
  204: {
    id: 204,
    name: CHANNEL_TYPES[204],
    icon: 'openai',
    defaultBaseUrl: '',
    hints: {
      baseUrl:
        '必填：供应商 Base URL，可填写到域名根路径或 /v1，例如 https://provider.example 或 https://provider.example/v1',
      key: '供应商 API Key（使用 Authorization: Bearer）',
      models: '填写对外模型名，并在模型映射中指定供应商模型 ID',
      other:
        '兼容 POST/GET /v1/videos；duration 必须显式传正整数。模型价格按每秒价格填写，实际费用 = 模型价格 × duration；失败或超时自动退款',
    },
  },
  205: {
    id: 205,
    name: CHANNEL_TYPES[205],
    icon: 'openai',
    defaultBaseUrl: 'https://zcbservice.aizfw.cn/kyyReactApiServer',
    hints: {
      baseUrl: 'Default: https://zcbservice.aizfw.cn/kyyReactApiServer',
      key: 'Globalaiopc 控制台分发的 API Key',
      models:
        '模型列表填写对外名称，并在“模型映射”中映射到 Model Center 模型：videos、videos_stable、videos_stable_fast、videos_pro 或 videos_pro_fast',
      other:
        '客户端统一调用 /v1/videos 或 /v1/video/generations；上游提交使用 /v2/model-center/tasks，查询使用 /v2/model-center/tasks/{id}；支持图片、视频、音频参考素材映射，模型价格按次填写',
    },
  },
  206: {
    id: 206,
    name: CHANNEL_TYPES[206],
    icon: 'openai',
    defaultBaseUrl: 'https://ai.artsapi.com',
    hints: {
      baseUrl:
        'Default: https://ai.artsapi.com（填写域名根路径，不要追加 /v1/video/generations）',
      key: 'ArtsAPI API Key（sk- 前缀，使用 Authorization: Bearer）',
      models:
        '可从 /v1/models 获取账号可用模型；模型由渠道配置和模型映射决定，不限制为固定列表',
      other:
        '提交和查询使用 /v1/video/generations；支持 images/image_urls、videos、audios 及 ArtsAPI 扩展参数透传。duration 必须显式传正整数，模型价格按每秒填写；完成响应中的 usage token 会记录，但 ModelPrice 仍按 duration 预扣结算',
    },
    validation: {
      keyFormat: /^sk-/,
      keyMinLength: 20,
    },
  },
  // ===== NewAPI default =====
  1: {
    id: 1,
    name: CHANNEL_TYPES[1],
    icon: 'openai',
    defaultBaseUrl: 'https://api.openai.com',
    requiresOrganization: true,
    hints: {
      baseUrl: 'Default: https://api.openai.com',
      key: 'Format: sk-...',
      models: 'gpt-4,gpt-4-turbo,gpt-3.5-turbo',
    },
    validation: {
      keyFormat: /^sk-/,
      keyMinLength: 20,
    },
  },
  3: {
    id: 3,
    name: CHANNEL_TYPES[3],
    icon: 'azure',
    requiresRegion: true,
    hints: {
      baseUrl: 'Azure OpenAI Endpoint',
      key: 'Azure API Key',
      models: 'Deployment names',
    },
  },
  14: {
    id: 14,
    name: CHANNEL_TYPES[14],
    icon: 'anthropic',
    defaultBaseUrl: 'https://api.anthropic.com',
    hints: {
      key: 'Format: sk-ant-...',
      models: 'claude-3-opus,claude-3-sonnet,claude-3-haiku',
    },
  },
  24: {
    id: 24,
    name: CHANNEL_TYPES[24],
    icon: 'google',
    hints: {
      key: 'Google API Key',
      models: 'gemini-pro,gemini-pro-vision',
    },
  },
  41: {
    id: 41,
    name: CHANNEL_TYPES[41],
    icon: 'google',
    requiresRegion: true,
    hints: {
      key: 'Service account JSON or API key',
      models: 'gemini-pro,gemini-1.5-pro',
      other: 'Region config: {"default": "us-central1"}',
    },
  },
  43: {
    id: 43,
    name: CHANNEL_TYPES[43],
    icon: 'deepseek',
    defaultBaseUrl: 'https://api.deepseek.com',
    hints: {
      key: 'DeepSeek API Key',
      models: 'deepseek-chat,deepseek-coder',
    },
  },
  20: {
    id: 20,
    name: CHANNEL_TYPES[20],
    icon: 'openrouter',
    defaultBaseUrl: 'https://openrouter.ai/api',
    hints: {
      key: 'OpenRouter API Key',
      models: 'Use model IDs from OpenRouter',
    },
  },
  56: {
    id: 56,
    name: CHANNEL_TYPES[56],
    icon: 'replicate',
    defaultBaseUrl: 'https://api.replicate.com',
    hints: {
      key: 'Replicate API Token',
      models: 'Replicate model IDs',
      baseUrl: 'Default: https://api.replicate.com',
    },
  },
  58: {
    id: 58,
    name: CHANNEL_TYPES[58],
    icon: 'newapi',
    hints: {
      baseUrl: 'Fallback base URL',
      key: 'Used by route auth templates',
      models: 'Models exposed by this channel',
    },
  },
}

/**
 * Get configuration for a channel type
 */
export function getChannelTypeConfig(type: number): ChannelTypeConfig {
  return (
    CHANNEL_TYPE_CONFIGS[type] || {
      id: type,
      name: CHANNEL_TYPES[type as keyof typeof CHANNEL_TYPES] || 'Unknown',
      icon: 'openai',
    }
  )
}

/**
 * Check if channel type requires organization field
 */
export function requiresOrganization(type: number): boolean {
  return CHANNEL_TYPE_CONFIGS[type]?.requiresOrganization || false
}

/**
 * Check if channel type requires region configuration
 */
export function requiresRegion(type: number): boolean {
  return CHANNEL_TYPE_CONFIGS[type]?.requiresRegion || false
}

/**
 * Get default base URL for channel type
 */
export function getDefaultBaseUrl(type: number): string {
  return CHANNEL_TYPE_CONFIGS[type]?.defaultBaseUrl || ''
}

/**
 * Get hints for channel type
 */
export function getChannelTypeHints(type: number) {
  return CHANNEL_TYPE_CONFIGS[type]?.hints || {}
}

/**
 * Validate API key format for channel type
 */
export function validateKeyFormat(type: number, key: string): boolean {
  const config = CHANNEL_TYPE_CONFIGS[type]
  if (!config?.validation) return true

  const { keyFormat, keyMinLength } = config.validation

  if (keyMinLength && key.length < keyMinLength) {
    return false
  }

  if (keyFormat && !keyFormat.test(key)) {
    return false
  }

  return true
}
