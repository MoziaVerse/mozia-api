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
  settings: SupplierResource & {
    enabled: boolean
    shadow: boolean
    canary_percent: number
  }
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

export type SupplierResourceKind =
  | 'supplier'
  | 'pool'
  | 'binding'
  | 'rule'
  | 'settings'
export type SupplierResource = {
  id?: number | string
  version: number
  runtime_revision: number
} & Record<string, unknown>
export type SupplierResourceResult = {
  resource: SupplierResource
  application: 'not_required' | 'applied' | 'pending'
  revision: number
  replayed: boolean
}
export type SupplierResourceStatus = {
  target_revision: number
  applied_revision: number
  application: 'applied' | 'pending'
}
export type SupplierRevision = {
  id: number
  resource_kind: string
  resource_id: string
  created_at: number
  applied_at: number
}

export function supplierResourcePath(
  kind: SupplierResourceKind,
  id?: string | number,
  supplierId?: number
): string {
  if (kind === 'settings') return '/api/supplier-routing/settings'
  if (kind === 'pool' && id === undefined) {
    return `/api/suppliers/${supplierId}/pools`
  }
  const paths = {
    supplier: '/api/suppliers',
    pool: '/api/supplier-pools',
    binding: '/api/supplier-bindings',
    rule: '/api/supplier-routing/rules',
  }
  return id === undefined
    ? paths[kind]
    : `${paths[kind]}/${encodeURIComponent(id)}`
}

export function supplierResourceETag(
  kind: SupplierResourceKind,
  resource: SupplierResource
): string {
  return `"${kind}-${resource.id ?? 'settings'}-v${resource.version}"`
}

export async function getSupplierResource(
  kind: SupplierResourceKind,
  id?: number | string
): Promise<SupplierResource> {
  const response = await api.get<Envelope<SupplierResource>>(
    supplierResourcePath(kind, id)
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function saveSupplierResource(input: {
  kind: SupplierResourceKind
  resource?: SupplierResource
  supplierId?: number
  payload: unknown
  idempotencyKey: string
}): Promise<SupplierResourceResult> {
  const path = supplierResourcePath(
    input.kind,
    input.resource?.id,
    input.supplierId
  )
  const response = input.resource
    ? await api.patch<Envelope<SupplierResourceResult>>(path, input.payload, {
        headers: {
          'If-Match': supplierResourceETag(input.kind, input.resource),
        },
      })
    : await api.post<Envelope<SupplierResourceResult>>(path, input.payload, {
        headers: { 'Idempotency-Key': input.idempotencyKey },
      })
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function deleteSupplierResource(
  kind: SupplierResourceKind,
  resource: SupplierResource
): Promise<SupplierResourceResult> {
  const response = await api.delete<Envelope<SupplierResourceResult>>(
    supplierResourcePath(kind, resource.id),
    { headers: { 'If-Match': supplierResourceETag(kind, resource) } }
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function getSupplierResourceStatus(): Promise<SupplierResourceStatus> {
  const response = await api.get<Envelope<SupplierResourceStatus>>(
    '/api/supplier-routing/status'
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}

export async function getSupplierRevisions(
  kind: SupplierResourceKind,
  id: string | number
): Promise<SupplierRevision[]> {
  const response = await api.get<Envelope<SupplierRevision[]>>(
    '/api/supplier-routing/revisions',
    { params: { resource_kind: kind, resource_id: id } }
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data ?? []
}

export async function restoreSupplierResource(
  kind: SupplierResourceKind,
  resource: SupplierResource,
  revision: number
): Promise<SupplierResourceResult> {
  const response = await api.post<Envelope<SupplierResourceResult>>(
    `${supplierResourcePath(kind, resource.id)}/restore`,
    { revision },
    { headers: { 'If-Match': supplierResourceETag(kind, resource) } }
  )
  if (!response.data.success) throw new Error(response.data.message)
  return response.data.data
}
