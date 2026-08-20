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
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import {
  buildTaskBillingConfig,
  createEnumValueDraft,
  createTaskDimensionDraft,
  validateTaskBillingDraft,
  type TaskBillingDraft,
  type TaskDimensionDraft,
} from './task-billing-config'

type TaskBillingEditorProps = {
  draft: TaskBillingDraft
  showJSON: boolean
  onChange: (draft: TaskBillingDraft) => void
  onShowJSONChange: (show: boolean) => void
}

const selectClassName =
  'border-input bg-background h-9 w-full rounded-md border px-3 text-sm'

export function TaskBillingEditor(props: TaskBillingEditorProps) {
  const { t } = useTranslation()
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
      <Tabs
        value={props.draft.mode}
        onValueChange={(mode) =>
          props.onChange({
            ...props.draft,
            mode: mode as TaskBillingDraft['mode'],
          })
        }
      >
        <TabsList className='grid w-full grid-cols-2'>
          <TabsTrigger value='per_second'>{t('Per-second')}</TabsTrigger>
          <TabsTrigger value='parametric'>{t('Multi-parameter')}</TabsTrigger>
        </TabsList>
      </Tabs>

      {props.draft.mode === 'per_second' ? (
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
      ) : (
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

      <Button
        type='button'
        variant='outline'
        className='w-fit'
        onClick={() => props.onShowJSONChange(!props.showJSON)}
      >
        <Code data-icon='inline-start' />
        {props.showJSON ? t('Hide JSON') : t('View JSON')}
      </Button>
      {props.showJSON && (
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
