import type { LaneKey, PricingMode } from './model-pricing-core'
import type { TaskBillingDraft } from './task-billing-config'

export type OfficialPricingDraft = {
  currency: 'USD' | 'CNY'
  sourceUrl: string
  verifiedAt: string
  noteMarkdown: string
  items: Record<string, string>
}

export type OfficialPriceOption = {
  key: string
  labelParts: string[]
}

export const createOfficialPricingDraft = (): OfficialPricingDraft => ({
  currency: 'USD',
  sourceUrl: '',
  verifiedAt: '',
  noteMarkdown: '',
  items: {},
})

export const parseOfficialPricingDraft = (
  raw?: string
): OfficialPricingDraft => {
  const fallback = createOfficialPricingDraft()
  if (!raw) return fallback
  try {
    const value = JSON.parse(raw) as Record<string, unknown>
    const rawItems =
      value.items && typeof value.items === 'object'
        ? (value.items as Record<string, unknown>)
        : {}
    return {
      currency: value.currency === 'CNY' ? 'CNY' : 'USD',
      sourceUrl: typeof value.source_url === 'string' ? value.source_url : '',
      verifiedAt:
        typeof value.verified_at === 'string' ? value.verified_at : '',
      noteMarkdown:
        typeof value.note_markdown === 'string' ? value.note_markdown : '',
      items: Object.fromEntries(
        Object.entries(rawItems)
          .filter(([, amount]) => typeof amount === 'number')
          .map(([key, amount]) => [key, String(amount)])
      ),
    }
  } catch {
    return fallback
  }
}

export const buildOfficialPricingConfig = (
  draft: OfficialPricingDraft
): string => {
  const items = Object.fromEntries(
    Object.entries(draft.items)
      .filter(([, amount]) => amount.trim() !== '')
      .map(([key, amount]) => [key, Number(amount)])
  )
  if (Object.keys(items).length === 0) return ''
  return JSON.stringify(
    {
      currency: draft.currency,
      source_url: draft.sourceUrl.trim(),
      verified_at: draft.verifiedAt,
      items,
      ...(draft.noteMarkdown.trim()
        ? { note_markdown: draft.noteMarkdown.trim() }
        : {}),
    },
    null,
    2
  )
}

export const validateOfficialPricingDraft = (
  draft: OfficialPricingDraft
): string | null => {
  const amounts = Object.values(draft.items).filter(
    (amount) => amount.trim() !== ''
  )
  if (amounts.length === 0) return null
  if (!draft.sourceUrl.trim()) return 'Official pricing source URL is required.'
  try {
    const source = new URL(draft.sourceUrl)
    if (source.protocol !== 'https:' && source.protocol !== 'http:') {
      return 'Official pricing source URL must use HTTP or HTTPS.'
    }
  } catch {
    return 'Official pricing source URL is invalid.'
  }
  if (!/^\d{4}-\d{2}-\d{2}$/.test(draft.verifiedAt)) {
    return 'Official pricing verification date is required.'
  }
  if (
    amounts.some((amount) => {
      const value = Number(amount)
      return !Number.isFinite(value) || value < 0
    })
  ) {
    return 'Official prices must be non-negative numbers.'
  }
  return null
}

const tokenLanes: Array<[LaneKey, string, string]> = [
  ['cache', 'token:cache_read', 'Cache read'],
  ['createCache', 'token:cache_write', 'Cache write'],
  ['image', 'token:image_input', 'Image input'],
  ['audioInput', 'token:audio_input', 'Audio input'],
  ['audioOutput', 'token:audio_output', 'Audio output'],
]

export const buildOfficialPriceOptions = (input: {
  pricingMode: PricingMode
  laneEnabled: Record<LaneKey, boolean>
  referenceVideoPrice: string
  taskBilling: TaskBillingDraft
}): OfficialPriceOption[] => {
  if (input.pricingMode === 'tiered_expr') return []

  if (input.pricingMode === 'per-token') {
    const base = [
      { key: 'token:input', labelParts: ['Input Token', '/ 1M Tokens'] },
      { key: 'token:output', labelParts: ['Output Token', '/ 1M Tokens'] },
      ...tokenLanes
        .filter(([lane]) => input.laneEnabled[lane])
        .map(([, key, label]) => ({
          key,
          labelParts: [label, '/ 1M Tokens'],
        })),
    ]
    if (!input.laneEnabled.videoInput) return base
    return base.flatMap((item) => [
      {
        key: `${item.key}:reference_video=false`,
        labelParts: [...item.labelParts, 'No reference video'],
      },
      {
        key: `${item.key}:reference_video=true`,
        labelParts: [...item.labelParts, 'With reference video'],
      },
    ])
  }

  const unit = input.pricingMode === 'per-request' ? 'request' : 'second'
  const baseKey = `task:${unit}`
  const hasReferenceVideo = input.referenceVideoPrice.trim() !== ''
  const variants = hasReferenceVideo
    ? [
        { suffix: ':reference_video=false', label: 'No reference video' },
        { suffix: ':reference_video=true', label: 'With reference video' },
      ]
    : [{ suffix: '', label: '' }]

  let options: OfficialPriceOption[] = []
  if (input.pricingMode === 'parametric') {
    const enumDimensions = input.taskBilling.dimensions.filter(
      (dimension) => dimension.kind === 'enum'
    )
    const durationDimensions = input.taskBilling.dimensions.filter(
      (dimension) =>
        dimension.kind === 'number' &&
        ['duration', 'seconds'].includes(dimension.name.trim().toLowerCase())
    )
    const isDirectlyComparable =
      durationDimensions.length === 1 &&
      input.taskBilling.dimensions.length === enumDimensions.length + 1
    if (isDirectlyComparable && enumDimensions.length === 0) {
      options = variants.map((variant) => ({
        key: `task:second${variant.suffix}`,
        labelParts: ['Per second', variant.label].filter(Boolean),
      }))
    } else if (isDirectlyComparable && enumDimensions.length === 1) {
      const dimension = enumDimensions[0]
      options = dimension.values.flatMap((value) =>
        variants.map((variant) => ({
          key: `task:second:${dimension.name.trim().toLowerCase()}=${value.value.trim().toLowerCase()}${variant.suffix}`,
          labelParts: [value.value.trim(), variant.label, '/ second'].filter(
            Boolean
          ),
        }))
      )
    }
  } else {
    options = variants.map((variant) => ({
      key: `${baseKey}${variant.suffix}`,
      labelParts: [
        input.pricingMode === 'per-request' ? 'Per request' : 'Per second',
        variant.label,
      ].filter(Boolean),
    }))
  }

  if (input.taskBilling.surcharge.enabled) {
    options.push({
      key: `surcharge:${input.taskBilling.surcharge.name.trim().toLowerCase()}`,
      labelParts: [
        'Additional',
        input.taskBilling.surcharge.name.trim(),
        '/ item',
      ],
    })
  }
  return options
}
