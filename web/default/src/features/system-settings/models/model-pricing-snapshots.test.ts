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
      billingMode: 'task-parameter',
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
      billingMode: '{}',
      billingExpr: '{}',
      taskBilling: '{}',
    })
    const translate = (key: string) => key

    assert.equal(row.videoInputRatio, '0.6086956521739131')
    assert.equal(getPriceDetail(row, translate), 'Reference video $28')
  })
})
