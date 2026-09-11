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

import type { SupplierConfigValues } from './lib/config-schema'

export type SupplierConfig = {
  revision: number
  enabled: boolean
  shadow: boolean
  canary_percent: number
  suppliers: SupplierConfigValues['suppliers'] | null
  pools: SupplierConfigValues['pools'] | null
  bindings: SupplierConfigValues['bindings'] | null
  rules: SupplierConfigValues['rules'] | null
}

export type SupplierRoutingData = {
  config: SupplierConfig
  revisions: { id: number; created_at: number; created_by: number }[]
  channels: { id: number; name: string; models: string; type: number }[]
}

export type SupplierAttempt = {
  id: number
  created_at: number
  request_id: string
  attempt: number
  supplier_id: number
  pool_id: number
  channel_id: number
  model: string
  kind: string
  status: string
  reason: string
  ttft_ms: number
  latency_ms: number
  input_tokens: number
  output_tokens: number
  cost: string
  currency: string
  cost_status: string
}

export type SupplierStat = {
  first_share_percent: number
  health_state: string
  health_scale: number
  priority_fallback: boolean
  outcome_class: string
  supplier_id: number
  pool_id: number
  model: string
  group_name: string
  kind: string
  status: string
  requests: number
  avg_ttft_ms: number
  avg_latency_ms: number
  output_tokens: number
}

export type SupplierStats = {
  hour_start: number
  rows: SupplierStat[]
  summary: {
    requests: number
    first_success: number
    final_success: number
    unresolved: number
  }
}

type Envelope<T> = { success: boolean; message: string; data: T }
const base = '/api/channel/supplier-routing'

export async function getSupplierRouting(): Promise<SupplierRoutingData> {
  const response = await api.get<Envelope<SupplierRoutingData>>(base)
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function saveSupplierRouting(
  config: SupplierConfig,
  validate = false
): Promise<SupplierConfig> {
  const payload = { expected_revision: config.revision, config }
  const response = validate
    ? await api.post<Envelope<SupplierConfig>>(`${base}/validate`, payload)
    : await api.put<Envelope<SupplierConfig>>(base, payload)
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function rollbackSupplierRouting(
  expectedRevision: number,
  rollbackRevision: number
): Promise<void> {
  const response = await api.put<Envelope<unknown>>(base, {
    expected_revision: expectedRevision,
    rollback_revision: rollbackRevision,
  })
  if (!response.data.success) throw new Error(response.data.message)
}

export async function getSupplierAttempts(): Promise<SupplierAttempt[]> {
  const response = await api.get<Envelope<SupplierAttempt[]>>(
    `${base}/attempts`
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data ?? []
}

export async function getSupplierStats(): Promise<SupplierStats> {
  const response = await api.get<Envelope<SupplierStats>>(`${base}/stats`)
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function reconcileSupplierAttempt(
  id: number,
  status: string,
  cost: string,
  currency: string,
  note: string
): Promise<void> {
  const response = await api.post<Envelope<unknown>>(
    `${base}/attempts/${id}/reconcile`,
    { status, cost, currency, note }
  )
  if (!response.data.success) throw new Error(response.data.message)
}

export async function previewSupplierModels(id: number): Promise<unknown> {
  const response = await api.get<Envelope<unknown>>(`${base}/models/${id}`)
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}
