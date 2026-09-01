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
import { splitBillingExprAndRequestRules } from '@/features/pricing/lib/billing-expr'

import { safeJsonParse } from '../utils/json-parser'
import { formatPricingNumber } from './pricing-format'

export type ModelPricingSnapshotInput = {
  modelPrice: string
  modelRatio: string
  cacheRatio: string
  createCacheRatio: string
  completionRatio: string
  imageRatio: string
  audioRatio: string
  audioCompletionRatio: string
  videoInputRatio: string
  referenceVideoPrice: string
  billingMode: string
  billingExpr: string
  taskBilling: string
  officialPricing?: string
}

export type ModelPricingSnapshot = {
  name: string
  price?: string
  ratio?: string
  cacheRatio?: string
  createCacheRatio?: string
  completionRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
  videoInputRatio?: string
  referenceVideoPrice?: string
  billingMode?: string
  billingExpr?: string
  requestRuleExpr?: string
  taskBilling?: string
  officialPricing?: string
  hasConflict: boolean
}

export type ModelRow = ModelPricingSnapshot & {
  saved?: ModelPricingSnapshot
  draft?: ModelPricingSnapshot
  isDraftChanged: boolean
  isDraftDeleted: boolean
  isDraftNew: boolean
}

export const hasPricingValue = (value?: string) =>
  value !== undefined && value !== ''

type TaskBillingRuleSummary = {
  mode?: string
  surcharge?: {
    free_count?: number
    unit_price?: number
  }
  token_prices?: {
    values?: Record<string, unknown>
  }
}

const getTaskBillingRuleSummary = (
  taskBilling?: string
): TaskBillingRuleSummary =>
  safeJsonParse<TaskBillingRuleSummary>(taskBilling || '', {
    fallback: {},
    context: 'task parameter billing summary',
    silent: true,
  })

const toNumberOrNull = (value?: string) => {
  if (!hasPricingValue(value)) return null
  const num = Number(value)
  return Number.isFinite(num) ? num : null
}

const ratioToPrice = (ratio?: string, denominator?: string) => {
  const ratioNumber = toNumberOrNull(ratio)
  const denominatorNumber = denominator ? toNumberOrNull(denominator) : 2
  if (ratioNumber === null || denominatorNumber === null) return ''
  return formatPricingNumber(ratioNumber * denominatorNumber)
}

export const getModeLabel = (mode?: string) => {
  if (mode === 'token_parametric') return 'Multi-parameter Token'
  if (mode === 'per_second') return 'Per-second'
  if (mode === 'parametric') return 'Multi-parameter'
  if (mode === 'per-request') return 'Per-request'
  if (mode === 'tiered_expr') return 'Expression'
  return 'Per-token'
}

export const getModeVariant = (
  mode?: string
): 'warning' | 'info' | 'success' => {
  if (
    mode === 'per_second' ||
    mode === 'parametric' ||
    mode === 'token_parametric'
  )
    return 'warning'
  if (mode === 'per-request') return 'warning'
  if (mode === 'tiered_expr') return 'info'
  return 'success'
}

const getExpressionSummary = (
  row: ModelPricingSnapshot,
  t: (key: string) => string
) => {
  const tierCount = (row.billingExpr?.match(/tier\(/g) || []).length
  if (tierCount > 0) {
    return `${t('Tiered pricing')} · ${tierCount} ${t('tiers')}`
  }
  return t('Expression pricing')
}

export const getPriceSummary = (
  row: ModelPricingSnapshot,
  t: (key: string) => string
) => {
  if (row.billingMode === 'tiered_expr') {
    return getExpressionSummary(row, t)
  }
  if (row.billingMode === 'per-request') {
    if (!row.price) return t('Unset price')
    return row.referenceVideoPrice
      ? `$${row.price} / ${t('request')} · ${t('Reference video')} $${row.referenceVideoPrice}`
      : `$${row.price} / ${t('request')}`
  }
  if (row.billingMode === 'token_parametric') {
    const count = Object.keys(
      getTaskBillingRuleSummary(row.taskBilling).token_prices?.values || {}
    ).length
    return `${t('Multi-parameter Token')} · ${count}`
  }
  if (row.billingMode === 'per_second' || row.billingMode === 'parametric') {
    if (!row.price) return t('Unset price')
    const rule = getTaskBillingRuleSummary(row.taskBilling)
    const baseSummary =
      rule.mode === 'per_second'
        ? `$${row.price} / ${t('second')}`
        : `$${row.price} / ${t('parameter')}`
    return row.referenceVideoPrice
      ? `${baseSummary} · ${t('Reference video')} $${row.referenceVideoPrice}`
      : baseSummary
  }

  const inputPrice = ratioToPrice(row.ratio)
  if (!inputPrice) return t('Unset price')

  const extraCount = [
    row.completionRatio,
    row.cacheRatio,
    row.createCacheRatio,
    row.imageRatio,
    row.audioRatio,
    row.audioCompletionRatio,
    row.videoInputRatio,
  ].filter(hasPricingValue).length

  return extraCount > 0
    ? `${t('Input')} $${inputPrice} · ${extraCount} ${t('extras')}`
    : `${t('Input')} $${inputPrice}`
}

export const getPriceDetail = (
  row: ModelPricingSnapshot,
  t: (key: string) => string
) => {
  if (row.billingMode === 'tiered_expr') {
    return row.requestRuleExpr
      ? t('Includes request rules')
      : t('Expression based')
  }
  if (row.billingMode === 'per-request') {
    return t('Fixed request price')
  }
  if (row.billingMode === 'token_parametric') {
    return t('USD price per 1M tokens.')
  }
  if (row.billingMode === 'per_second' || row.billingMode === 'parametric') {
    const surcharge = getTaskBillingRuleSummary(row.taskBilling).surcharge
    if (
      typeof surcharge?.free_count === 'number' &&
      typeof surcharge.unit_price === 'number'
    ) {
      return `${surcharge.free_count} ${t('free items')} · $${surcharge.unit_price} / ${t('additional item')}`
    }
    return t('Task parameter billing')
  }

  const inputPrice = ratioToPrice(row.ratio)
  if (!inputPrice) return t('No base input price')

  const details = [
    row.videoInputRatio &&
      `${t('Reference video')} $${ratioToPrice(row.videoInputRatio, inputPrice)}`,
    row.completionRatio &&
      `${t('Output')} $${ratioToPrice(row.completionRatio, inputPrice)}`,
    row.cacheRatio &&
      `${t('Cache')} $${ratioToPrice(row.cacheRatio, inputPrice)}`,
    row.createCacheRatio &&
      `${t('Cache write')} $${ratioToPrice(row.createCacheRatio, inputPrice)}`,
  ]
    .filter(Boolean)
    .slice(0, 2)

  return details.length > 0 ? details.join(' · ') : t('Base input price only')
}

export const buildModelSnapshots = ({
  modelPrice,
  modelRatio,
  cacheRatio,
  createCacheRatio,
  completionRatio,
  imageRatio,
  audioRatio,
  audioCompletionRatio,
  videoInputRatio,
  referenceVideoPrice,
  billingMode,
  billingExpr,
  taskBilling,
  officialPricing,
}: ModelPricingSnapshotInput): ModelPricingSnapshot[] => {
  const priceMap = safeJsonParse<Record<string, number>>(modelPrice, {
    fallback: {},
    context: 'model prices',
  })
  const ratioMap = safeJsonParse<Record<string, number>>(modelRatio, {
    fallback: {},
    context: 'model ratios',
  })
  const cacheMap = safeJsonParse<Record<string, number>>(cacheRatio, {
    fallback: {},
    context: 'cache ratios',
  })
  const createCacheMap = safeJsonParse<Record<string, number>>(
    createCacheRatio,
    { fallback: {}, context: 'create cache ratios' }
  )
  const completionMap = safeJsonParse<Record<string, number>>(completionRatio, {
    fallback: {},
    context: 'completion ratios',
  })
  const imageMap = safeJsonParse<Record<string, number>>(imageRatio, {
    fallback: {},
    context: 'image ratios',
  })
  const audioMap = safeJsonParse<Record<string, number>>(audioRatio, {
    fallback: {},
    context: 'audio ratios',
  })
  const audioCompletionMap = safeJsonParse<Record<string, number>>(
    audioCompletionRatio,
    { fallback: {}, context: 'audio completion ratios' }
  )
  const videoInputMap = safeJsonParse<Record<string, number>>(videoInputRatio, {
    fallback: {},
    context: 'reference video input ratios',
  })
  const referenceVideoPriceMap = safeJsonParse<Record<string, number>>(
    referenceVideoPrice,
    { fallback: {}, context: 'reference video prices' }
  )
  const billingModeMap = safeJsonParse<Record<string, string>>(billingMode, {
    fallback: {},
    context: 'billing mode',
  })
  const billingExprMap = safeJsonParse<Record<string, string>>(billingExpr, {
    fallback: {},
    context: 'billing expression',
  })
  const taskBillingMap = safeJsonParse<Record<string, TaskBillingRuleSummary>>(
    taskBilling,
    {
      fallback: {},
      context: 'task parameter billing',
    }
  )
  const officialPricingMap = safeJsonParse<Record<string, unknown>>(
    officialPricing,
    {
      fallback: {},
      context: 'official pricing display',
    }
  )

  const modelNames = new Set([
    ...Object.keys(priceMap),
    ...Object.keys(ratioMap),
    ...Object.keys(cacheMap),
    ...Object.keys(createCacheMap),
    ...Object.keys(completionMap),
    ...Object.keys(imageMap),
    ...Object.keys(audioMap),
    ...Object.keys(audioCompletionMap),
    ...Object.keys(videoInputMap),
    ...Object.keys(referenceVideoPriceMap),
    ...Object.keys(billingModeMap),
    ...Object.keys(billingExprMap),
    ...Object.keys(taskBillingMap),
    ...Object.keys(officialPricingMap),
  ])

  return [...modelNames].map((name) => {
    const price = priceMap[name]?.toString() || ''
    const ratio = ratioMap[name]?.toString() || ''
    const cache = cacheMap[name]?.toString() || ''
    const createCache = createCacheMap[name]?.toString() || ''
    const completion = completionMap[name]?.toString() || ''
    const image = imageMap[name]?.toString() || ''
    const audio = audioMap[name]?.toString() || ''
    const audioCompletion = audioCompletionMap[name]?.toString() || ''
    const videoInput = videoInputMap[name]?.toString() || ''
    const referenceVideo = referenceVideoPriceMap[name]?.toString() || ''
    const officialPricingForModel = officialPricingMap[name]
      ? JSON.stringify(officialPricingMap[name], null, 2)
      : ''

    const modeForModel = billingModeMap[name]
    const taskBillingRule = taskBillingMap[name]
    if (taskBillingRule) {
      const taskMode =
        taskBillingRule.mode === 'per_second' ||
        taskBillingRule.mode === 'token_parametric'
          ? taskBillingRule.mode
          : 'parametric'
      return {
        name,
        billingMode: taskMode,
        taskBilling: JSON.stringify(taskBillingRule, null, 2),
        price,
        ratio,
        cacheRatio: cache,
        createCacheRatio: createCache,
        completionRatio: completion,
        imageRatio: image,
        audioRatio: audio,
        audioCompletionRatio: audioCompletion,
        videoInputRatio: videoInput,
        referenceVideoPrice: referenceVideo,
        officialPricing: officialPricingForModel,
        hasConflict: false,
      }
    }
    if (modeForModel === 'tiered_expr') {
      const fullExpr = billingExprMap[name] || ''
      const { billingExpr: pureExpr, requestRuleExpr } =
        splitBillingExprAndRequestRules(fullExpr)
      return {
        name,
        billingMode: 'tiered_expr',
        billingExpr: pureExpr,
        requestRuleExpr,
        price,
        ratio,
        cacheRatio: cache,
        createCacheRatio: createCache,
        completionRatio: completion,
        imageRatio: image,
        audioRatio: audio,
        audioCompletionRatio: audioCompletion,
        videoInputRatio: videoInput,
        referenceVideoPrice: referenceVideo,
        officialPricing: officialPricingForModel,
        hasConflict: false,
      }
    }

    return {
      name,
      price,
      ratio,
      cacheRatio: cache,
      createCacheRatio: createCache,
      completionRatio: completion,
      imageRatio: image,
      audioRatio: audio,
      audioCompletionRatio: audioCompletion,
      videoInputRatio: videoInput,
      referenceVideoPrice: referenceVideo,
      officialPricing: officialPricingForModel,
      billingMode: price !== '' ? 'per-request' : 'per-token',
      hasConflict:
        price !== '' &&
        (ratio !== '' ||
          completion !== '' ||
          cache !== '' ||
          createCache !== '' ||
          image !== '' ||
          audio !== '' ||
          audioCompletion !== '' ||
          videoInput !== ''),
    }
  })
}

export const getSnapshotSignature = (snapshot?: ModelPricingSnapshot) => {
  if (!snapshot) return ''
  return JSON.stringify({
    price: snapshot.price || '',
    ratio: snapshot.ratio || '',
    cacheRatio: snapshot.cacheRatio || '',
    createCacheRatio: snapshot.createCacheRatio || '',
    completionRatio: snapshot.completionRatio || '',
    imageRatio: snapshot.imageRatio || '',
    audioRatio: snapshot.audioRatio || '',
    audioCompletionRatio: snapshot.audioCompletionRatio || '',
    videoInputRatio: snapshot.videoInputRatio || '',
    referenceVideoPrice: snapshot.referenceVideoPrice || '',
    billingMode: snapshot.billingMode || 'per-token',
    billingExpr: snapshot.billingExpr || '',
    requestRuleExpr: snapshot.requestRuleExpr || '',
    taskBilling: snapshot.taskBilling || '',
    officialPricing: snapshot.officialPricing || '',
  })
}
