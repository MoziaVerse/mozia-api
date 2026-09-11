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

const id = z.number().int().positive().max(Number.MAX_SAFE_INTEGER)
const name = z.string().min(1).max(191)
const limits = z.object({
  concurrency: z.number().int().min(0).max(10000),
  rpm: z.number().int().min(0).max(100000),
  tpm: z.number().int().min(0).max(1000000000),
})
const model = z.object({
  name,
  version: name,
  context_tokens: z.number().int().min(1).max(10000000),
  max_output_tokens: z.number().int().min(1).max(10000000),
  tools: z.boolean(),
  json: z.boolean(),
  limits,
})

// Validate editable fields locally; the server owns cross-resource publication validation.
export const supplierConfigSchema = z.object({
  revision: z.number().int().min(0),
  enabled: z.boolean(),
  shadow: z.boolean(),
  canary_percent: z.number().int().min(0).max(100),
  suppliers: z
    .array(
      z.object({
        id,
        name,
        enabled: z.boolean(),
        region: z.string().optional(),
        contact: z.string().max(4000).optional(),
        data_policy: z.string().max(16000).optional(),
        terms: z.string().max(16000).optional(),
      })
    )
    .max(128),
  pools: z
    .array(
      z.object({
        id,
        supplier_id: id,
        name,
        failure_domain: name,
        enabled: z.boolean(),
        limits: limits.extend({
          concurrency: limits.shape.concurrency.min(1),
          rpm: limits.shape.rpm.min(1),
          tpm: limits.shape.tpm.min(1),
        }),
        max_execution_seconds: z.number().int().min(1).max(3600),
        input_safety_percent: z.number().int().min(100).max(200),
        acceptance: z.string().optional(),
        models: z.array(model).min(1).max(128),
      })
    )
    .max(128),
  bindings: z
    .array(z.object({ channel_id: id, model: name, pool_id: id }))
    .max(256),
  rules: z
    .array(
      z.object({
        id: z.string().min(1).max(96),
        model: name,
        group: z.string().max(191).optional(),
        user_id: z.number().int().min(0).optional(),
        mode: z.enum(['capacity', 'share', 'failover']),
        targets: z
          .array(
            z.object({
              supplier_id: id,
              weight: z.number().int().min(1).max(10000),
            })
          )
          .min(1)
          .max(32),
        max_attempts: z.number().int().min(1).max(10),
        timeout_seconds: z.number().int().min(1).max(3600),
        max_supplier_percent: z.number().int().min(0).max(100).optional(),
        health: z.object({
          window_seconds: z.number().int().min(10).max(3600),
          min_samples: z.number().int().min(1).max(10000),
          failure_percent: z.number().int().min(1).max(100),
          max_ttft_ms: z.number().int().min(1),
          cooldown_seconds: z.number().int().min(1).max(3600),
          trial_percent: z.number().int().min(1).max(100),
        }),
      })
    )
    .max(128),
})

export type SupplierConfigValues = z.infer<typeof supplierConfigSchema>

export function newSupplierModel(
  modelName = ''
): SupplierConfigValues['pools'][number]['models'][number] {
  return {
    name: modelName,
    version: '',
    context_tokens: 32000,
    max_output_tokens: 4096,
    tools: false,
    json: false,
    limits: { concurrency: 0, rpm: 0, tpm: 0 },
  }
}

export function newSupplierRule(
  id: string,
  supplierId: number,
  modelName = ''
): SupplierConfigValues['rules'][number] {
  return {
    id,
    model: modelName,
    group: '',
    user_id: 0,
    mode: 'capacity',
    targets: [{ supplier_id: supplierId, weight: 100 }],
    max_attempts: 2,
    timeout_seconds: 120,
    max_supplier_percent: 0,
    health: {
      window_seconds: 60,
      min_samples: 10,
      failure_percent: 20,
      max_ttft_ms: 5000,
      cooldown_seconds: 30,
      trial_percent: 10,
    },
  }
}

export function supplierRoutingMode(
  config: Pick<SupplierConfigValues, 'enabled' | 'shadow' | 'canary_percent'>
) {
  if (!config.enabled) return 'legacy'
  if (config.shadow || config.canary_percent === 0) return 'observe'
  if (config.canary_percent === 100) return 'active'
  return 'canary'
}
