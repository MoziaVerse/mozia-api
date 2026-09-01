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
import { Code, Plus, Trash2 } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  buildTaskBillingConfig,
  createEnumValueDraft,
  createTaskDimensionDraft,
  createTaskTokenPriceDraft,
  validateTaskBillingDraft,
  type TaskBillingDraft,
  type TaskDimensionDraft,
  type TaskSurchargeDraft,
  type TaskTokenPriceDraft,
  type TaskTokenPricingDraft,
} from './task-billing-config'

type TaskBillingEditorProps = {
  draft: TaskBillingDraft
  onChange: (draft: TaskBillingDraft) => void
}

const selectClassName =
  'border-input bg-background h-9 w-full rounded-md border px-3 text-sm'

export function TaskBillingEditor(props: TaskBillingEditorProps) {
  const { t } = useTranslation()
  const surchargeSwitchId = useId()
  const [showJSON, setShowJSON] = useState(false)
  const validationError = validateTaskBillingDraft(props.draft)

  const updateDuration = (patch: Partial<TaskDimensionDraft>) => {
    props.onChange({
      ...props.draft,
      duration: { ...props.draft.duration, ...patch },
    })
  }

  const updateDimension = (id: string, patch: Partial<TaskDimensionDraft>) => {
    props.onChange({
      ...props.draft,
      dimensions: props.draft.dimensions.map((dimension) =>
        dimension.id === id ? { ...dimension, ...patch } : dimension
      ),
    })
  }

  const updateSurcharge = (patch: Partial<TaskSurchargeDraft>) => {
    props.onChange({
      ...props.draft,
      surcharge: { ...props.draft.surcharge, ...patch },
    })
  }

  const updateTokenPrices = (patch: Partial<TaskTokenPricingDraft>) => {
    props.onChange({
      ...props.draft,
      tokenPrices: { ...props.draft.tokenPrices, ...patch },
    })
  }

  const updateTokenPrice = (id: string, patch: Partial<TaskTokenPriceDraft>) =>
    updateTokenPrices({
      values: props.draft.tokenPrices.values.map((price) =>
        price.id === id ? { ...price, ...patch } : price
      ),
    })

  const renderNumericFields = (
    dimension: TaskDimensionDraft,
    onChange: (patch: Partial<TaskDimensionDraft>) => void
  ) => (
    <div className='grid gap-4 sm:grid-cols-3'>
      <Field>
        <FieldLabel>{t('Default value')}</FieldLabel>
        <Input
          inputMode='decimal'
          value={dimension.defaultValue}
          onChange={(event) => onChange({ defaultValue: event.target.value })}
        />
        <FieldDescription>
          {t('Used when none of the parameter paths is present.')}
        </FieldDescription>
      </Field>
      <Field>
        <FieldLabel>{t('Billing unit')}</FieldLabel>
        <Input
          inputMode='decimal'
          value={dimension.unit}
          onChange={(event) => onChange({ unit: event.target.value })}
        />
        <FieldDescription>
          {t('Divides the rounded numeric value into billable units.')}
        </FieldDescription>
      </Field>
      <Field>
        <FieldLabel>{t('Rounding')}</FieldLabel>
        <select
          className={selectClassName}
          value={dimension.round}
          onChange={(event) =>
            onChange({
              round: event.target.value as TaskDimensionDraft['round'],
            })
          }
        >
          <option value='none'>{t('None')}</option>
          <option value='ceil'>{t('Round up')}</option>
          <option value='floor'>{t('Round down')}</option>
          <option value='nearest'>{t('Nearest')}</option>
        </select>
        <FieldDescription>
          {t('Applied before calculating this dimension multiplier.')}
        </FieldDescription>
      </Field>
    </div>
  )

  const renderEnumFields = (dimension: TaskDimensionDraft) => (
    <FieldGroup className='gap-3'>
      <Field>
        <FieldLabel>{t('Default value')}</FieldLabel>
        <Input
          value={dimension.defaultValue}
          onChange={(event) =>
            updateDimension(dimension.id, {
              defaultValue: event.target.value,
            })
          }
        />
        <FieldDescription>
          {t('Must match one configured option when provided.')}
        </FieldDescription>
      </Field>
      <Field>
        <div className='flex items-center justify-between gap-3'>
          <FieldLabel>{t('Options and multipliers')}</FieldLabel>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() =>
              updateDimension(dimension.id, {
                values: [...dimension.values, createEnumValueDraft()],
              })
            }
          >
            <Plus data-icon='inline-start' />
            {t('Add option')}
          </Button>
        </div>
        <FieldDescription>
          {t('Maps each accepted request value to a price multiplier.')}
        </FieldDescription>
        <div className='grid gap-2'>
          {dimension.values.map((option) => (
            <div
              key={option.id}
              className='grid grid-cols-[minmax(0,1fr)_minmax(100px,140px)_36px] items-center gap-2'
            >
              <Input
                aria-label={t('Option value')}
                placeholder='1080p'
                value={option.value}
                onChange={(event) =>
                  updateDimension(dimension.id, {
                    values: dimension.values.map((candidate) =>
                      candidate.id === option.id
                        ? { ...candidate, value: event.target.value }
                        : candidate
                    ),
                  })
                }
              />
              <Input
                aria-label={t('Multiplier')}
                inputMode='decimal'
                placeholder='1'
                value={option.multiplier}
                onChange={(event) =>
                  updateDimension(dimension.id, {
                    values: dimension.values.map((candidate) =>
                      candidate.id === option.id
                        ? { ...candidate, multiplier: event.target.value }
                        : candidate
                    ),
                  })
                }
              />
              <Button
                type='button'
                variant='ghost'
                size='icon'
                aria-label={t('Remove option')}
                onClick={() =>
                  updateDimension(dimension.id, {
                    values: dimension.values.filter(
                      (candidate) => candidate.id !== option.id
                    ),
                  })
                }
              >
                <Trash2 className='text-destructive h-4 w-4' />
              </Button>
            </div>
          ))}
        </div>
      </Field>
    </FieldGroup>
  )

  return (
    <FieldGroup className='gap-5'>
      {props.draft.mode === 'token_parametric' && (
        <FieldGroup className='gap-4'>
          <div className='grid gap-4 sm:grid-cols-2'>
            <Field>
              <FieldLabel>{t('Parameter paths')}</FieldLabel>
              <Input
                value={props.draft.tokenPrices.paths}
                onChange={(event) =>
                  updateTokenPrices({ paths: event.target.value })
                }
              />
            </Field>
            <Field>
              <FieldLabel>{t('Default value')}</FieldLabel>
              <Input
                placeholder='720p'
                value={props.draft.tokenPrices.defaultValue}
                onChange={(event) =>
                  updateTokenPrices({ defaultValue: event.target.value })
                }
              />
            </Field>
          </div>
          <Field>
            <div className='flex items-center justify-between gap-3'>
              <FieldLabel>{t('Multi-parameter Token')}</FieldLabel>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() =>
                  updateTokenPrices({
                    values: [
                      ...props.draft.tokenPrices.values,
                      createTaskTokenPriceDraft(),
                    ],
                  })
                }
              >
                <Plus data-icon='inline-start' />
                {t('Add option')}
              </Button>
            </div>
            <FieldDescription>{t('Price per 1M tokens.')}</FieldDescription>
            <div className='grid gap-2'>
              {props.draft.tokenPrices.values.map((price) => (
                <div
                  key={price.id}
                  className='grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] items-center gap-2 sm:grid-cols-[minmax(90px,1fr)_minmax(110px,1fr)_minmax(110px,1fr)_36px]'
                >
                  <Input
                    aria-label={t('Option value')}
                    placeholder='1080p'
                    value={price.resolution}
                    onChange={(event) =>
                      updateTokenPrice(price.id, {
                        resolution: event.target.value,
                      })
                    }
                  />
                  <Input
                    aria-label={t('Standard price')}
                    inputMode='decimal'
                    placeholder='38.25'
                    value={price.standard}
                    onChange={(event) =>
                      updateTokenPrice(price.id, {
                        standard: event.target.value,
                      })
                    }
                  />
                  <Input
                    aria-label={t('Reference video price')}
                    inputMode='decimal'
                    placeholder='23.25'
                    value={price.referenceVideo}
                    onChange={(event) =>
                      updateTokenPrice(price.id, {
                        referenceVideo: event.target.value,
                      })
                    }
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    aria-label={t('Remove option')}
                    onClick={() =>
                      updateTokenPrices({
                        values: props.draft.tokenPrices.values.filter(
                          (candidate) => candidate.id !== price.id
                        ),
                      })
                    }
                  >
                    <Trash2 className='text-destructive h-4 w-4' />
                  </Button>
                </div>
              ))}
            </div>
          </Field>
        </FieldGroup>
      )}
      {props.draft.mode === 'per_second' && (
        <FieldGroup className='gap-4'>
          <Field>
            <FieldLabel>{t('Duration paths')}</FieldLabel>
            <Input
              value={props.draft.duration.paths}
              onChange={(event) =>
                updateDuration({ paths: event.target.value })
              }
            />
            <FieldDescription>
              {t(
                'Uses the first available request field, in the listed order.'
              )}
            </FieldDescription>
          </Field>
          {renderNumericFields(props.draft.duration, updateDuration)}
        </FieldGroup>
      )}
      {props.draft.mode === 'parametric' && (
        <FieldGroup className='gap-4'>
          <div className='flex items-start justify-between gap-4'>
            <p className='text-muted-foreground text-sm'>
              {t('The base price is multiplied by every configured dimension.')}
            </p>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                props.onChange({
                  ...props.draft,
                  dimensions: [
                    ...props.draft.dimensions,
                    createTaskDimensionDraft({
                      name: '',
                      paths: '',
                      defaultValue: '',
                      round: 'none',
                    }),
                  ],
                })
              }
            >
              <Plus data-icon='inline-start' />
              {t('Add dimension')}
            </Button>
          </div>

          {props.draft.dimensions.map((dimension, index) => (
            <section key={dimension.id} className='rounded-md border p-4'>
              <div className='mb-4 flex items-center justify-between gap-3'>
                <h4 className='text-sm font-medium'>
                  {t('Dimension')} {index + 1}
                </h4>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  aria-label={t('Remove dimension')}
                  onClick={() =>
                    props.onChange({
                      ...props.draft,
                      dimensions: props.draft.dimensions.filter(
                        (candidate) => candidate.id !== dimension.id
                      ),
                    })
                  }
                >
                  <Trash2 className='text-destructive h-4 w-4' />
                </Button>
              </div>

              <FieldGroup className='gap-4'>
                <div className='grid gap-4 sm:grid-cols-2'>
                  <Field>
                    <FieldLabel>{t('Dimension name')}</FieldLabel>
                    <Input
                      placeholder='resolution'
                      value={dimension.name}
                      onChange={(event) =>
                        updateDimension(dimension.id, {
                          name: event.target.value,
                        })
                      }
                    />
                    <FieldDescription>
                      {t('Uniquely identifies this pricing dimension.')}
                    </FieldDescription>
                  </Field>
                  <Field>
                    <FieldLabel>{t('Dimension type')}</FieldLabel>
                    <select
                      className={selectClassName}
                      value={dimension.kind}
                      onChange={(event) => {
                        const kind = event.target
                          .value as TaskDimensionDraft['kind']
                        updateDimension(dimension.id, {
                          kind,
                          values:
                            kind === 'enum' && dimension.values.length === 0
                              ? [createEnumValueDraft()]
                              : dimension.values,
                        })
                      }}
                    >
                      <option value='number'>{t('Numeric')}</option>
                      <option value='enum'>{t('Enumeration')}</option>
                    </select>
                    <FieldDescription>
                      {t(
                        'Numeric values scale by quantity; enumerations use fixed multipliers.'
                      )}
                    </FieldDescription>
                  </Field>
                </div>
                <Field>
                  <FieldLabel>{t('Parameter paths')}</FieldLabel>
                  <Input
                    placeholder='resolution, metadata.resolution'
                    value={dimension.paths}
                    onChange={(event) =>
                      updateDimension(dimension.id, {
                        paths: event.target.value,
                      })
                    }
                  />
                  <FieldDescription>
                    {t(
                      'Uses the first available request field, in the listed order.'
                    )}
                  </FieldDescription>
                </Field>
                {dimension.kind === 'number'
                  ? renderNumericFields(dimension, (patch) =>
                      updateDimension(dimension.id, patch)
                    )
                  : renderEnumFields(dimension)}
              </FieldGroup>
            </section>
          ))}
        </FieldGroup>
      )}

      {props.draft.mode !== 'token_parametric' && (
        <section className='rounded-md border p-4'>
          <div className='flex items-start justify-between gap-4'>
            <div className='grid gap-1'>
              <FieldLabel htmlFor={surchargeSwitchId}>
                {t('Per-item surcharge')}
              </FieldLabel>
              <FieldDescription>
                {t(
                  'Adds a fixed price for each counted item after the free allowance.'
                )}
              </FieldDescription>
            </div>
            <Switch
              id={surchargeSwitchId}
              checked={props.draft.surcharge.enabled}
              onCheckedChange={(enabled) => updateSurcharge({ enabled })}
              aria-label={t('Enable per-item surcharge')}
            />
          </div>

          {props.draft.surcharge.enabled && (
            <FieldGroup className='mt-5 gap-4'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <Field>
                  <FieldLabel>{t('Surcharge name')}</FieldLabel>
                  <Input
                    placeholder='input_images'
                    value={props.draft.surcharge.name}
                    onChange={(event) =>
                      updateSurcharge({ name: event.target.value })
                    }
                  />
                  <FieldDescription>
                    {t('Identifies this surcharge in billing logs.')}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel>{t('Item types')}</FieldLabel>
                  <Input
                    placeholder='image, image_url'
                    value={props.draft.surcharge.itemTypes}
                    onChange={(event) =>
                      updateSurcharge({ itemTypes: event.target.value })
                    }
                  />
                  <FieldDescription>
                    {t(
                      'Optional comma-separated object types; strings are always counted.'
                    )}
                  </FieldDescription>
                </Field>
              </div>
              <Field>
                <FieldLabel>{t('Item count paths')}</FieldLabel>
                <Input
                  placeholder='conditions, content, images'
                  value={props.draft.surcharge.paths}
                  onChange={(event) =>
                    updateSurcharge({ paths: event.target.value })
                  }
                />
                <FieldDescription>
                  {t(
                    'Uses the first non-empty request field, in the listed order.'
                  )}
                </FieldDescription>
              </Field>
              <div className='grid gap-4 sm:grid-cols-2'>
                <Field>
                  <FieldLabel>{t('Free item count')}</FieldLabel>
                  <Input
                    inputMode='numeric'
                    value={props.draft.surcharge.freeCount}
                    onChange={(event) =>
                      updateSurcharge({ freeCount: event.target.value })
                    }
                  />
                  <FieldDescription>
                    {t('Items at or below this count do not add a surcharge.')}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel>{t('Price per additional item')}</FieldLabel>
                  <Input
                    inputMode='decimal'
                    placeholder='0.2'
                    value={props.draft.surcharge.unitPrice}
                    onChange={(event) =>
                      updateSurcharge({ unitPrice: event.target.value })
                    }
                  />
                  <FieldDescription>
                    {t(
                      'Added to the base task price for every item above the free count.'
                    )}
                  </FieldDescription>
                </Field>
              </div>
            </FieldGroup>
          )}
        </section>
      )}

      <Button
        type='button'
        variant='outline'
        className='w-fit'
        onClick={() => setShowJSON((shown) => !shown)}
      >
        <Code data-icon='inline-start' />
        {showJSON ? t('Hide JSON') : t('View JSON')}
      </Button>
      {showJSON && (
        <pre className='bg-muted overflow-x-auto rounded-md border p-3 font-mono text-xs leading-5'>
          {JSON.stringify(buildTaskBillingConfig(props.draft), null, 2)}
        </pre>
      )}
      {validationError && (
        <p className='text-destructive text-sm' role='alert'>
          {t(validationError)}
        </p>
      )}
    </FieldGroup>
  )
}
