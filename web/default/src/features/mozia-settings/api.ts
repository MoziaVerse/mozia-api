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

import { api } from '@/lib/api'

import type {
  ApiEnvelope,
  MoziaUserModelRatio,
  MoziaUserModelRatioPayload,
} from './types'

const USER_MODEL_RATIO_ENDPOINT = '/api/mozia/user-model-ratio'

function unwrapResponse<T>(response: ApiEnvelope<T>): T {
  if (!response.success) {
    throw new Error(response.message)
  }
  return response.data
}

export async function getMoziaUserModelRatios() {
  const response = await api.get<ApiEnvelope<MoziaUserModelRatio[]>>(
    `${USER_MODEL_RATIO_ENDPOINT}/`,
    {
      skipBusinessError: true,
      skipErrorHandler: true,
    }
  )
  return unwrapResponse(response.data)
}

export async function saveMoziaUserModelRatio(
  payload: MoziaUserModelRatioPayload
) {
  const response = await api.post<ApiEnvelope<MoziaUserModelRatio>>(
    `${USER_MODEL_RATIO_ENDPOINT}/`,
    payload,
    {
      skipBusinessError: true,
      skipErrorHandler: true,
    }
  )
  return unwrapResponse(response.data)
}

export async function deleteMoziaUserModelRatio(rule: MoziaUserModelRatio) {
  const targetParams =
    rule.scope === 'model'
      ? { model: rule.model }
      : { channel_id: rule.channel_id }
  const response = await api.delete<ApiEnvelope<null>>(
    `${USER_MODEL_RATIO_ENDPOINT}/${rule.user_id}`,
    {
      params: {
        scope: rule.scope,
        ...targetParams,
      },
      skipBusinessError: true,
      skipErrorHandler: true,
    }
  )
  return unwrapResponse(response.data)
}
