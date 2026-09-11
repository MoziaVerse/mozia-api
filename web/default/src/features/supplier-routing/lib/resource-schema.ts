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
import { z } from 'zod'

import type {
  SupplierResourceKind,
  SupplierRoutingData,
  SupplierResource,
} from '../api'
import {
  supplierConfigSchema,
  newSupplierModel,
  newSupplierRule,
  type SupplierConfigValues,
} from './config-schema'

const schemas = {
  supplier: supplierConfigSchema.shape.suppliers.element
    .omit({ id: true })
    .strict(),
  pool: supplierConfigSchema.shape.pools.element
    .omit({ id: true, supplier_id: true })
    .strict(),
  rule: supplierConfigSchema.shape.rules.element.omit({ id: true }).strict(),
  settings: supplierConfigSchema
    .pick({ enabled: true, shadow: true, canary_percent: true })
    .strict(),
}
export function supplierResourceSchema(kind: SupplierResourceKind) {
  return schemas[kind]
}

export function editableSupplierResource(
  kind: SupplierResourceKind,
  resource: Record<string, unknown>
): Record<string, unknown> {
  return Object.fromEntries(
    Object.keys(schemas[kind].shape)
      .filter((key) => key in resource)
      .map((key) => [key, resource[key]])
  )
}

export function parseSupplierResourceJSON(
  kind: SupplierResourceKind,
  raw: string,
  draft = false
): Record<string, unknown> {
  const value: unknown = JSON.parse(raw)
  const parsed = schemas[kind].safeParse(value)
  if (!parsed.success) {
    const structural = parsed.error.issues.filter(
      (issue) =>
        issue.code !== 'too_small' &&
        issue.code !== 'too_big' &&
        !(issue.code === 'invalid_type' && issue.expected === 'int')
    )
    if (!draft || structural.length) {
      throw new z.ZodError(draft ? structural : parsed.error.issues)
    }
  }
  return value as Record<string, unknown>
}

export function supplierResourceFormValues(
  data: SupplierRoutingData,
  kind: SupplierResourceKind,
  resource?: SupplierResource,
  supplierId?: number
): SupplierConfigValues {
  const values: SupplierConfigValues = {
    ...data.config,
    suppliers: data.config.suppliers ?? [],
    pools: data.config.pools ?? [],
    bindings: data.config.bindings ?? [],
    rules: data.config.rules ?? [],
  }
  if (kind === 'supplier') {
    values.suppliers = [
      resource
        ? (resource as unknown as SupplierConfigValues['suppliers'][number])
        : {
            id: 1,
            name: '',
            enabled: false,
            region: '',
            contact: '',
            data_policy: '',
            terms: '',
          },
    ]
  }
  if (kind === 'pool') {
    values.pools = [
      resource
        ? (resource as unknown as SupplierConfigValues['pools'][number])
        : {
            id: 0,
            supplier_id: supplierId ?? 0,
            name: '',
            failure_domain: '',
            enabled: false,
            limits: { concurrency: 10, rpm: 100, tpm: 100000 },
            max_execution_seconds: 120,
            input_safety_percent: 110,
            acceptance: '',
            models: [newSupplierModel()],
            bindings: [],
          },
    ]
    values.pools[0] = {
      ...values.pools[0],
      bindings: values.pools[0].bindings ?? [],
    }
  }
  if (kind === 'rule') {
    values.rules = [
      resource
        ? (resource as unknown as SupplierConfigValues['rules'][number])
        : newSupplierRule(
            'new',
            values.suppliers[0]?.id ?? 0,
            values.bindings[0]?.model ?? ''
          ),
    ]
  }
  if (kind === 'settings') {
    Object.assign(
      values,
      editableSupplierResource(kind, resource ?? data.settings)
    )
  }
  return values
}

// Keep unsupported/occupied channels visible with a reason instead of hiding them.
export function supplierModelChannels(
  data: SupplierRoutingData,
  pool: SupplierConfigValues['pools'][number],
  model: string
) {
  const owners = new Map(
    (data.config.pools ?? []).map((p) => [p.id, p.supplier_id])
  )
  const occupied = new Map<number, 'supplier' | 'pool'>()
  for (const binding of data.config.bindings ?? []) {
    if (binding.pool_id === pool.id) continue
    if (owners.get(binding.pool_id) !== pool.supplier_id) {
      occupied.set(binding.channel_id, 'supplier')
    } else if (binding.model === model && !occupied.has(binding.channel_id)) {
      occupied.set(binding.channel_id, 'pool')
    }
  }
  return data.channels
    .filter((channel) =>
      channel.models
        .split(',')
        .map((name) => name.trim())
        .includes(model)
    )
    .map((channel) => ({
      ...channel,
      reason:
        channel.type !== 1
          ? ('unsupported' as const)
          : occupied.get(channel.id),
    }))
}

export function supplierFormResource(
  kind: SupplierResourceKind,
  values: SupplierConfigValues
): Record<string, unknown> {
  const objects = {
    supplier: values.suppliers[0],
    pool: values.pools[0],
    rule: values.rules[0],
    settings: values,
  }
  return editableSupplierResource(kind, objects[kind])
}
