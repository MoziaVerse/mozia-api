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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  buildTaskBillingConfig,
  parseTaskBillingDraft,
  validateTaskBillingDraft,
} from './task-billing-config'

describe('task billing visual configuration', () => {
  test('round-trips numeric and enumeration dimensions from an existing rule', () => {
    const rule = {
      version: 1 as const,
      mode: 'parametric' as const,
      dimensions: [
        {
          name: 'duration',
          kind: 'number' as const,
          paths: ['duration', 'seconds'],
          default: 5,
          unit: 1,
          round: 'ceil' as const,
        },
        {
          name: 'resolution',
          kind: 'enum' as const,
          paths: ['resolution', 'metadata.resolution'],
          default: '720p',
          values: { '480p': 1, '720p': 2.15, '1080p': 5.36 },
        },
      ],
    }

    const draft = parseTaskBillingDraft(JSON.stringify(rule))

    assert.equal(validateTaskBillingDraft(draft), null)
    assert.deepEqual(buildTaskBillingConfig(draft), rule)
  })

  test('round-trips an existing per-item surcharge without losing fields', () => {
    const rule = {
      version: 1 as const,
      mode: 'per_second' as const,
      duration: {
        name: 'duration',
        kind: 'number' as const,
        paths: ['duration', 'seconds', 'metadata.duration'],
        default: 5,
        unit: 1,
        round: 'ceil' as const,
      },
      surcharge: {
        name: 'input_images',
        kind: 'item_count' as const,
        paths: ['conditions', 'content', 'images'],
        item_types: ['image', 'image_url'],
        free_count: 5,
        unit_price: 0.2,
      },
    }

    const draft = parseTaskBillingDraft(JSON.stringify(rule))

    assert.equal(draft.surcharge.enabled, true)
    assert.equal(draft.surcharge.freeCount, '5')
    assert.equal(draft.surcharge.unitPrice, '0.2')
    assert.equal(validateTaskBillingDraft(draft), null)
    assert.deepEqual(buildTaskBillingConfig(draft), rule)
  })

  test('rejects ambiguous dimensions and invalid enumeration multipliers', () => {
    const draft = parseTaskBillingDraft(`{
      "version": 1,
      "mode": "parametric",
      "dimensions": [
        {"name":"quality","kind":"enum","paths":["quality"],"values":{"hd":1}},
        {"name":"quality","kind":"number","paths":["duration"],"unit":1}
      ]
    }`)

    assert.equal(
      validateTaskBillingDraft(draft),
      'Dimension names must be unique.'
    )

    draft.dimensions[1].name = 'duration'
    draft.dimensions[0].values[0].multiplier = '0'
    assert.equal(
      validateTaskBillingDraft(draft),
      'Enumeration multipliers must be greater than zero.'
    )
  })

  test('rejects invalid surcharge allowances and prices', () => {
    const draft = parseTaskBillingDraft(`{
      "version": 1,
      "mode": "per_second",
      "duration": {"name":"duration","kind":"number","paths":["duration"],"unit":1},
      "surcharge": {"name":"input_images","kind":"item_count","paths":["images"],"free_count":5,"unit_price":0.2}
    }`)

    draft.surcharge.freeCount = '1.5'
    assert.equal(
      validateTaskBillingDraft(draft),
      'The free item count must be a non-negative integer.'
    )

    draft.surcharge.freeCount = '5'
    draft.surcharge.unitPrice = '0'
    assert.equal(
      validateTaskBillingDraft(draft),
      'The surcharge unit price must be greater than zero.'
    )
  })
})
