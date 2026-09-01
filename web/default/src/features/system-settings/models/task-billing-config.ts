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
import { nanoid } from 'nanoid'

type TaskBillingMode = 'per_second' | 'parametric' | 'token_parametric'
type TaskDimensionKind = 'number' | 'enum'
type TaskDimensionRound = 'none' | 'ceil' | 'floor' | 'nearest'

type TaskEnumValueDraft = {
  id: string
  value: string
  multiplier: string
}

export type TaskDimensionDraft = {
  id: string
  name: string
  kind: TaskDimensionKind
  paths: string
  defaultValue: string
  unit: string
  round: TaskDimensionRound
  values: TaskEnumValueDraft[]
}

export type TaskSurchargeDraft = {
  enabled: boolean
  name: string
  paths: string
  itemTypes: string
  freeCount: string
  unitPrice: string
}

export type TaskTokenPriceDraft = {
  id: string
  resolution: string
  standard: string
  referenceVideo: string
}

export type TaskTokenPricingDraft = {
  paths: string
  defaultValue: string
  values: TaskTokenPriceDraft[]
}

export type TaskBillingDraft = {
  mode: TaskBillingMode
  duration: TaskDimensionDraft
  dimensions: TaskDimensionDraft[]
  surcharge: TaskSurchargeDraft
  tokenPrices: TaskTokenPricingDraft
}

export const createEnumValueDraft = (
  value = '',
  multiplier = '1'
): TaskEnumValueDraft => ({
  id: nanoid(8),
  value,
  multiplier,
})

export const createTaskDimensionDraft = (
  overrides: Partial<Omit<TaskDimensionDraft, 'id'>> = {}
): TaskDimensionDraft => ({
  id: nanoid(8),
  name: 'duration',
  kind: 'number',
  paths: 'duration, seconds',
  defaultValue: '5',
  unit: '1',
  round: 'ceil',
  values: [],
  ...overrides,
})

export const createTaskSurchargeDraft = (
  overrides: Partial<TaskSurchargeDraft> = {}
): TaskSurchargeDraft => ({
  enabled: false,
  name: 'input_images',
  paths:
    'conditions, metadata.conditions, content, images, image, input_reference',
  itemTypes: 'image, image_url',
  freeCount: '0',
  unitPrice: '',
  ...overrides,
})

export const createTaskTokenPriceDraft = (
  resolution = '',
  standard = '',
  referenceVideo = ''
): TaskTokenPriceDraft => ({
  id: nanoid(8),
  resolution,
  standard,
  referenceVideo,
})

export const createTaskBillingDraft = (): TaskBillingDraft => ({
  mode: 'per_second',
  duration: createTaskDimensionDraft(),
  dimensions: [createTaskDimensionDraft()],
  surcharge: createTaskSurchargeDraft(),
  tokenPrices: {
    paths: 'resolution, metadata.resolution',
    defaultValue: '',
    values: [createTaskTokenPriceDraft()],
  },
})

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

const parseDimension = (value: unknown): TaskDimensionDraft | null => {
  if (!isRecord(value)) return null
  const kind: TaskDimensionKind = value.kind === 'enum' ? 'enum' : 'number'
  const values = isRecord(value.values)
    ? Object.entries(value.values).map(([option, multiplier]) =>
        createEnumValueDraft(option, String(multiplier))
      )
    : []

  return createTaskDimensionDraft({
    name: typeof value.name === 'string' ? value.name : '',
    kind,
    paths: Array.isArray(value.paths)
      ? value.paths
          .filter((path): path is string => typeof path === 'string')
          .join(', ')
      : '',
    defaultValue:
      typeof value.default === 'string' || typeof value.default === 'number'
        ? String(value.default)
        : '',
    unit: typeof value.unit === 'number' ? String(value.unit) : '1',
    round:
      value.round === 'ceil' ||
      value.round === 'floor' ||
      value.round === 'nearest'
        ? value.round
        : 'none',
    values,
  })
}

const parseSurcharge = (value: unknown): TaskSurchargeDraft => {
  if (!isRecord(value) || value.kind !== 'item_count') {
    return createTaskSurchargeDraft()
  }

  return createTaskSurchargeDraft({
    enabled: true,
    name: typeof value.name === 'string' ? value.name : '',
    paths: Array.isArray(value.paths)
      ? value.paths
          .filter((path): path is string => typeof path === 'string')
          .join(', ')
      : '',
    itemTypes: Array.isArray(value.item_types)
      ? value.item_types
          .filter(
            (itemType): itemType is string => typeof itemType === 'string'
          )
          .join(', ')
      : '',
    freeCount:
      typeof value.free_count === 'number' ? String(value.free_count) : '0',
    unitPrice:
      typeof value.unit_price === 'number' ? String(value.unit_price) : '',
  })
}

const parseTokenPrices = (value: unknown): TaskTokenPricingDraft | null => {
  if (!isRecord(value) || !isRecord(value.values)) return null
  return {
    paths: Array.isArray(value.paths)
      ? value.paths
          .filter((path): path is string => typeof path === 'string')
          .join(', ')
      : '',
    defaultValue: typeof value.default === 'string' ? value.default : '',
    values: Object.entries(value.values).map(([resolution, price]) => {
      const item = isRecord(price) ? price : {}
      return createTaskTokenPriceDraft(
        resolution,
        typeof item.standard === 'number' ? String(item.standard) : '',
        typeof item.reference_video === 'number'
          ? String(item.reference_video)
          : ''
      )
    }),
  }
}

export const parseTaskBillingDraft = (raw: string): TaskBillingDraft => {
  const fallback = createTaskBillingDraft()
  if (!raw) return fallback

  try {
    const config: unknown = JSON.parse(raw)
    if (!isRecord(config)) return fallback
    const surcharge = parseSurcharge(config.surcharge)

    if (config.mode === 'per_second') {
      const duration = parseDimension(config.duration)
      return duration
        ? {
            ...fallback,
            duration: {
              ...duration,
              name: duration.name || 'duration',
              kind: 'number',
            },
            surcharge,
          }
        : fallback
    }

    if (config.mode === 'parametric' && Array.isArray(config.dimensions)) {
      const dimensions = config.dimensions
        .map(parseDimension)
        .filter((dimension): dimension is TaskDimensionDraft => !!dimension)
      return {
        ...fallback,
        mode: 'parametric',
        dimensions: dimensions.length > 0 ? dimensions : fallback.dimensions,
        surcharge,
      }
    }

    if (config.mode === 'token_parametric') {
      const tokenPrices = parseTokenPrices(config.token_prices)
      return tokenPrices
        ? { ...fallback, mode: 'token_parametric', tokenPrices }
        : fallback
    }
  } catch {
    return fallback
  }

  return fallback
}

const parsePaths = (paths: string) =>
  paths
    .split(',')
    .map((path) => path.trim())
    .filter(Boolean)

const buildDimension = (draft: TaskDimensionDraft) => {
  if (draft.kind === 'enum') {
    const values = Object.fromEntries(
      draft.values.map((option) => [
        option.value.trim(),
        Number(option.multiplier),
      ])
    )
    return {
      name: draft.name.trim(),
      kind: 'enum',
      paths: parsePaths(draft.paths),
      ...(draft.defaultValue.trim()
        ? { default: draft.defaultValue.trim() }
        : {}),
      values,
    }
  }

  return {
    name: draft.name.trim(),
    kind: 'number',
    paths: parsePaths(draft.paths),
    ...(draft.defaultValue.trim()
      ? { default: Number(draft.defaultValue) }
      : {}),
    unit: Number(draft.unit),
    round: draft.round,
  }
}

const buildSurcharge = (draft: TaskSurchargeDraft) => {
  if (!draft.enabled) return {}

  const itemTypes = parsePaths(draft.itemTypes)
  return {
    surcharge: {
      name: draft.name.trim(),
      kind: 'item_count',
      paths: parsePaths(draft.paths),
      ...(itemTypes.length > 0 ? { item_types: itemTypes } : {}),
      free_count: Number(draft.freeCount),
      unit_price: Number(draft.unitPrice),
    },
  }
}

export const buildTaskBillingConfig = (draft: TaskBillingDraft) => {
  if (draft.mode === 'per_second') {
    return {
      version: 1,
      mode: 'per_second',
      duration: buildDimension(draft.duration),
      ...buildSurcharge(draft.surcharge),
    }
  }

  if (draft.mode === 'token_parametric') {
    return {
      version: 1,
      mode: 'token_parametric',
      token_prices: {
        paths: parsePaths(draft.tokenPrices.paths),
        ...(draft.tokenPrices.defaultValue.trim()
          ? { default: draft.tokenPrices.defaultValue.trim() }
          : {}),
        values: Object.fromEntries(
          draft.tokenPrices.values.map((price) => [
            price.resolution.trim(),
            {
              standard: Number(price.standard),
              ...(price.referenceVideo.trim()
                ? { reference_video: Number(price.referenceVideo) }
                : {}),
            },
          ])
        ),
      },
    }
  }

  return {
    version: 1,
    mode: 'parametric',
    dimensions: draft.dimensions.map(buildDimension),
    ...buildSurcharge(draft.surcharge),
  }
}

const isPositiveNumber = (value: string) => {
  const number = Number(value)
  return Number.isFinite(number) && number > 0
}

export const validateTaskBillingDraft = (
  draft: TaskBillingDraft
): string | null => {
  if (draft.mode === 'token_parametric') {
    if (parsePaths(draft.tokenPrices.paths).length === 0) {
      return 'Every dimension requires at least one parameter path.'
    }
    if (draft.tokenPrices.values.length === 0) {
      return 'Enumeration dimensions require at least one option.'
    }
    const resolutions = new Set<string>()
    for (const price of draft.tokenPrices.values) {
      const resolution = price.resolution.trim().toLowerCase()
      if (!resolution) return 'Enumeration options cannot be empty.'
      if (resolutions.has(resolution)) {
        return 'Enumeration options must be unique.'
      }
      resolutions.add(resolution)
      if (
        !isPositiveNumber(price.standard) ||
        (price.referenceVideo.trim() && !isPositiveNumber(price.referenceVideo))
      ) {
        return 'Enumeration multipliers must be greater than zero.'
      }
    }
    if (
      draft.tokenPrices.defaultValue.trim() &&
      !resolutions.has(draft.tokenPrices.defaultValue.trim().toLowerCase())
    ) {
      return 'The enumeration default must match a configured option.'
    }
    return null
  }

  const dimensions =
    draft.mode === 'per_second' ? [draft.duration] : draft.dimensions
  if (dimensions.length === 0) return 'At least one dimension is required.'

  const names = new Set<string>()
  for (const dimension of dimensions) {
    const name = dimension.name.trim()
    if (!name) return 'Every dimension requires a name.'
    if (names.has(name)) return 'Dimension names must be unique.'
    names.add(name)
    if (parsePaths(dimension.paths).length === 0) {
      return 'Every dimension requires at least one parameter path.'
    }

    if (dimension.kind === 'number') {
      if (!isPositiveNumber(dimension.unit)) {
        return 'Numeric dimension units must be greater than zero.'
      }
      if (
        dimension.defaultValue.trim() &&
        !isPositiveNumber(dimension.defaultValue)
      ) {
        return 'Numeric default values must be greater than zero.'
      }
      continue
    }

    if (dimension.values.length === 0) {
      return 'Enumeration dimensions require at least one option.'
    }
    const options = new Set<string>()
    for (const option of dimension.values) {
      const value = option.value.trim().toLowerCase()
      if (!value) return 'Enumeration options cannot be empty.'
      if (options.has(value)) return 'Enumeration options must be unique.'
      options.add(value)
      if (!isPositiveNumber(option.multiplier)) {
        return 'Enumeration multipliers must be greater than zero.'
      }
    }
    if (
      dimension.defaultValue.trim() &&
      !options.has(dimension.defaultValue.trim().toLowerCase())
    ) {
      return 'The enumeration default must match a configured option.'
    }
  }

  if (draft.surcharge.enabled) {
    if (!draft.surcharge.name.trim()) {
      return 'The item surcharge requires a name.'
    }
    if (parsePaths(draft.surcharge.paths).length === 0) {
      return 'The item surcharge requires at least one parameter path.'
    }
    const freeCount = Number(draft.surcharge.freeCount)
    if (!Number.isInteger(freeCount) || freeCount < 0) {
      return 'The free item count must be a non-negative integer.'
    }
    if (!isPositiveNumber(draft.surcharge.unitPrice)) {
      return 'The surcharge unit price must be greater than zero.'
    }
  }

  return null
}
