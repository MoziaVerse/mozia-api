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

export type ChannelCostMode =
  | 'per_token'
  | 'per_request'
  | 'per_second'
  | 'parametric'
  | 'tiered_expr'

export type ChannelCostConfig = {
  items?: Record<string, number>
  base_price?: number
  reference_video_price?: number
  billing_expr?: string
  task_billing?: Record<string, unknown>
}

export type ChannelCostRecord = {
  id: number
  channel_id: number
  model_name: string
  currency: 'CNY' | 'USD'
  mode: ChannelCostMode
  note: string
  config: ChannelCostConfig
}

export type ChannelCostData = {
  items: ChannelCostRecord[]
  channels: Array<{
    id: number
    name: string
    models: string
  }>
  models: string[]
}

type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

export async function getChannelCosts(): Promise<ChannelCostData> {
  const response = await api.get<ApiResponse<ChannelCostData>>(
    '/api/mozia/model-pricing/channel-costs'
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function saveChannelCost(
  cost: Omit<ChannelCostRecord, 'id'>
): Promise<ChannelCostRecord> {
  const response = await api.put<ApiResponse<ChannelCostRecord>>(
    '/api/mozia/model-pricing/channel-costs',
    cost
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function deleteChannelCost(id: number): Promise<void> {
  const response = await api.delete<ApiResponse<unknown>>(
    `/api/mozia/model-pricing/channel-costs/${id}`
  )
  if (!response.data.success) throw new Error(response.data.message)
}
