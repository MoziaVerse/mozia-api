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

export type TaskBillingMode = 'per_second' | 'parametric'
export type TaskDimensionKind = 'number' | 'enum'
export type TaskDimensionRound = 'none' | 'ceil' | 'floor' | 'nearest'

export type TaskEnumValueDraft = {
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

export type TaskBillingDraft = {
  mode: TaskBillingMode
  duration: TaskDimensionDraft
  dimensions: TaskDimensionDraft[]
}

type TaskBillingDimension = {
  name: string
  kind: TaskDimensionKind
  paths: string[]
  default?: number | string
  unit?: number
  round?: TaskDimensionRound
  values?: Record<string, number>
}

export type TaskBillingConfig = {
  version: 1
  mode: TaskBillingMode
  duration?: TaskBillingDimension
  dimensions?: TaskBillingDimension[]
}

const createEnumValueDraft = (
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

export const createTaskBillingDraft = (): TaskBillingDraft => ({
  mode: 'per_second',
  duration: createTaskDimensionDraft(),
  dimensions: [createTaskDimensionDraft()],
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

export const parseTaskBillingDraft = (raw: string): TaskBillingDraft => {
  const fallback = createTaskBillingDraft()
  if (!raw) return fallback

  try {
    const config: unknown = JSON.parse(raw)
    if (!isRecord(config)) return fallback

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
      }
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

const buildDimension = (draft: TaskDimensionDraft): TaskBillingDimension => {
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

export const buildTaskBillingConfig = (
  draft: TaskBillingDraft
): TaskBillingConfig => {
  if (draft.mode === 'per_second') {
    return {
      version: 1,
      mode: 'per_second',
      duration: buildDimension(draft.duration),
    }
  }

  return {
    version: 1,
    mode: 'parametric',
    dimensions: draft.dimensions.map(buildDimension),
  }
}

const isPositiveNumber = (value: string) => {
  const number = Number(value)
  return Number.isFinite(number) && number > 0
}

export const validateTaskBillingDraft = (
  draft: TaskBillingDraft
): string | null => {
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

  return null
}

export { createEnumValueDraft }
