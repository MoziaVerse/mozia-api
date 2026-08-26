/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

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
  buildModelSnapshots,
  getPriceDetail,
  getPriceSummary,
} from './model-pricing-snapshots'

describe('task billing pricing summaries', () => {
  test('shows the per-second price and item surcharge in the model list', () => {
    const row = {
      name: 'minimax/minimax-h3-2k',
      price: '0.9',
      billingMode: 'per_second',
      taskBilling: JSON.stringify({
        mode: 'per_second',
        surcharge: {
          free_count: 5,
          unit_price: 0.2,
        },
      }),
      hasConflict: false,
    }
    const translate = (key: string) => key

    assert.equal(getPriceSummary(row, translate), '$0.9 / second')
    assert.equal(
      getPriceDetail(row, translate),
      '5 free items · $0.2 / additional item'
    )
  })

  test('maps stored task rules to separate pricing modes', () => {
    const rows = buildModelSnapshots({
      modelPrice: '{"per-second-model":0.9,"parameter-model":1.2}',
      modelRatio: '{}',
      cacheRatio: '{}',
      createCacheRatio: '{}',
      completionRatio: '{}',
      imageRatio: '{}',
      audioRatio: '{}',
      audioCompletionRatio: '{}',
      videoInputRatio: '{}',
      referenceVideoPrice: '{"per-second-model":0.7}',
      billingMode: '{}',
      billingExpr: '{}',
      taskBilling:
        '{"per-second-model":{"mode":"per_second"},"parameter-model":{"mode":"parametric"}}',
    })

    assert.equal(
      rows.find((row) => row.name === 'per-second-model')?.billingMode,
      'per_second'
    )
    assert.equal(
      rows.find((row) => row.name === 'parameter-model')?.billingMode,
      'parametric'
    )
    const perSecondRow = rows.find((row) => row.name === 'per-second-model')
    assert.ok(perSecondRow)
    assert.equal(
      getPriceSummary(perSecondRow, (key) => key),
      '$0.9 / second · Reference video $0.7'
    )
  })
})

describe('reference video token pricing', () => {
  test('shows the configured conditional token price', () => {
    const [row] = buildModelSnapshots({
      modelPrice: '{}',
      modelRatio: '{"doubao/seedance-2.0-pro-480p":23}',
      cacheRatio: '{}',
      createCacheRatio: '{}',
      completionRatio: '{}',
      imageRatio: '{}',
      audioRatio: '{}',
      audioCompletionRatio: '{}',
      videoInputRatio: '{"doubao/seedance-2.0-pro-480p":0.6086956521739131}',
      referenceVideoPrice: '{}',
      billingMode: '{}',
      billingExpr: '{}',
      taskBilling: '{}',
    })
    const translate = (key: string) => key

    assert.equal(row.videoInputRatio, '0.6086956521739131')
    assert.equal(getPriceDetail(row, translate), 'Reference video $28')
  })
})
