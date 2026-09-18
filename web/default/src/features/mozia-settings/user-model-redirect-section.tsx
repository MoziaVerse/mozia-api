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

import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useId, useState } from 'react'
import { useFieldArray, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SettingsCard } from '@/features/system-settings/components/settings-card'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import {
  deleteMoziaUserModelRedirect,
  getMoziaRoutingTargets,
  getMoziaUserModelRedirects,
  saveMoziaUserModelRedirect,
} from './api'
import type {
  MoziaUserModelRedirect,
  MoziaUserModelRedirectPayload,
  RouteCondition,
} from './types'

const queryKey = ['mozia', 'user-model-redirects'] as const
const conditionSchema = z
  .object({
    operator: z.enum(['equals', 'exists', 'has_video']),
    path: z.string(),
    valueText: z.string(),
  })
  .superRefine((condition, ctx) => {
    if (
      condition.operator !== 'has_video' &&
      !/^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$/.test(condition.path)
    ) {
      ctx.addIssue({
        code: 'custom',
        path: ['path'],
        message: 'Enter a field path, such as thinking.type',
      })
    }
    if (condition.operator === 'equals') {
      try {
        const value: unknown = JSON.parse(condition.valueText)
        if (
          typeof value === 'number' &&
          (!Number.isFinite(value) ||
            (Number.isInteger(value) && !Number.isSafeInteger(value)))
        ) {
          throw new Error()
        }
        if (
          value !== null &&
          !['string', 'number', 'boolean'].includes(typeof value)
        ) {
          throw new Error()
        }
      } catch {
        ctx.addIssue({
          code: 'custom',
          path: ['valueText'],
          message: 'Enter a JSON string, number, boolean or null',
        })
      }
    }
  })
const formSchema = z.object({
  all_users: z.boolean(),
  sso_sub: z.string(),
  source_model: z.string().trim().min(1),
  target_model: z.string().trim().min(1),
  target_channel_id: z.number().int().min(0),
  priority: z.number().int().min(0).max(10000),
  endpoint: z.string(),
  disabled: z.boolean(),
  only_thinking_disabled: z.boolean(),
  seamless: z.boolean(),
  conditions: z.array(conditionSchema).max(8),
})
type FormValues = z.infer<typeof formSchema>
const defaults: FormValues = {
  all_users: false,
  sso_sub: '',
  source_model: '',
  target_model: '',
  target_channel_id: 0,
  priority: 100,
  endpoint: '',
  disabled: false,
  only_thinking_disabled: false,
  seamless: false,
  conditions: [],
}
const selectClass =
  'border-input bg-background h-9 w-full rounded-md border px-3 text-sm'

export function MoziaUserModelRedirectSection() {
  const { t } = useTranslation()
  const formId = useId()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<MoziaUserModelRedirect | null>(null)
  const [deleteTarget, setDeleteTarget] =
    useState<MoziaUserModelRedirect | null>(null)
  const form = useForm<FormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: defaults,
  })
  const conditions = useFieldArray({
    control: form.control,
    name: 'conditions',
  })
  const values = form.watch()
  const rulesQuery = useQuery({ queryKey, queryFn: getMoziaUserModelRedirects })
  const targetsQuery = useQuery({
    queryKey: ['mozia', 'routing-targets'],
    queryFn: getMoziaRoutingTargets,
  })
  const targets = targetsQuery.data ?? []
  const target = targets.find(
    (channel) => channel.id === values.target_channel_id
  )
  const models = target
    ? target.models
    : [...new Set(targets.flatMap((channel) => channel.models))]
  const resetForm = () => {
    setEditing(null)
    form.reset(defaults)
  }
  const onError = (error: unknown) =>
    toast.error(error instanceof Error ? error.message : t('Request failed'))
  const saveMutation = useMutation({
    mutationFn: saveMoziaUserModelRedirect,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey })
      resetForm()
      toast.success(t('Routing rule saved'))
    },
    onError,
  })
  const deleteMutation = useMutation({
    mutationFn: deleteMoziaUserModelRedirect,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey })
      resetForm()
      setDeleteTarget(null)
      toast.success(t('Routing rule deleted'))
    },
    onError,
  })
  const submit = (data: FormValues) => {
    if (!data.all_users && !editing?.user_id && !data.sso_sub.trim()) {
      form.setError('sso_sub', { message: 'Select a user or all users' })
      return
    }
    const payload: MoziaUserModelRedirectPayload = {
      ...data,
      id: editing?.id || crypto.randomUUID(),
      user_id: data.all_users ? 0 : (editing?.user_id ?? 0),
      sso_sub: data.all_users ? '' : data.sso_sub.trim(),
      conditions: data.conditions.map((condition): RouteCondition => {
        if (condition.operator === 'has_video') return { operator: 'has_video' }
        if (condition.operator === 'exists') {
          return { operator: 'exists', path: condition.path }
        }
        return {
          operator: 'equals',
          path: condition.path,
          value: JSON.parse(condition.valueText) as RouteCondition['value'],
        }
      }),
    }
    saveMutation.mutate(payload)
  }
  const rules = rulesQuery.data ?? []
  return (
    <SettingsSection title={t('Conditional Routing')}>
      <SettingsCard
        title={t('Conditional Routing')}
        description={t(
          'Match the original request once. Higher priorities run first; all conditions must match. A specified channel never falls back to another channel.'
        )}
      >
        <form onSubmit={form.handleSubmit(submit)} className='mb-6 space-y-4'>
          <fieldset disabled={saveMutation.isPending} className='space-y-4'>
            <div className='flex flex-wrap gap-4'>
              <label className='flex items-center gap-2 text-sm'>
                <input type='checkbox' {...form.register('all_users')} />
                {t('All users')}
              </label>
              <label className='flex items-center gap-2 text-sm'>
                <input type='checkbox' {...form.register('disabled')} />
                {t('Disabled')}
              </label>
            </div>
            {!values.all_users && (
              <div>
                <label htmlFor={`${formId}-user`} className='text-sm'>
                  {t('SSO subject')}
                </label>
                {editing?.user_id ? (
                  <p className='text-muted-foreground text-sm'>
                    {editing.username || editing.user_id}
                  </p>
                ) : (
                  <Input id={`${formId}-user`} {...form.register('sso_sub')} />
                )}
                {form.formState.errors.sso_sub && (
                  <p role='alert' className='text-destructive text-sm'>
                    {t('Select a user or all users')}
                  </p>
                )}
              </div>
            )}
            <div className='grid gap-4 md:grid-cols-2'>
              <div>
                <label htmlFor={`${formId}-source`} className='text-sm'>
                  {t('Source model')}
                </label>
                <Input
                  id={`${formId}-source`}
                  required
                  {...form.register('source_model')}
                />
              </div>
              <div>
                <label htmlFor={`${formId}-priority`} className='text-sm'>
                  {t('Priority (higher first)')}
                </label>
                <Input
                  id={`${formId}-priority`}
                  type='number'
                  min={0}
                  max={10000}
                  required
                  {...form.register('priority', { valueAsNumber: true })}
                />
              </div>
              <div>
                <label htmlFor={`${formId}-endpoint`} className='text-sm'>
                  {t('Request endpoint')}
                </label>
                <select
                  id={`${formId}-endpoint`}
                  className={selectClass}
                  {...form.register('endpoint')}
                >
                  <option value=''>{t('All endpoints')}</option>
                  {[
                    '/v1/chat/completions',
                    '/v1/messages',
                    '/v1/responses',
                    '/pg/chat/completions',
                  ].map((endpoint) => (
                    <option key={endpoint} value={endpoint}>
                      {endpoint}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor={`${formId}-channel`} className='text-sm'>
                  {t('Target channel')}
                </label>
                <select
                  id={`${formId}-channel`}
                  className={selectClass}
                  {...form.register('target_channel_id', {
                    valueAsNumber: true,
                  })}
                >
                  <option value={0}>{t('Automatic channel selection')}</option>
                  {values.target_channel_id > 0 && !target && (
                    <option value={values.target_channel_id}>
                      #{values.target_channel_id} ({t('Unavailable')})
                    </option>
                  )}
                  {targets.map((channel) => (
                    <option
                      key={channel.id}
                      value={channel.id}
                      disabled={channel.status !== 1}
                    >
                      #{channel.id} {channel.name}
                      {channel.status === 1 ? '' : ` (${t('Disabled')})`}
                    </option>
                  ))}
                </select>
              </div>
              <div>
                <label htmlFor={`${formId}-target`} className='text-sm'>
                  {t('Target model')}
                </label>
                {values.target_channel_id > 0 ? (
                  <select
                    id={`${formId}-target`}
                    className={selectClass}
                    required
                    {...form.register('target_model')}
                  >
                    <option value=''>{t('Select a model')}</option>
                    {values.target_model &&
                      !models.includes(values.target_model) && (
                        <option value={values.target_model} disabled>
                          {values.target_model} ({t('Unavailable')})
                        </option>
                      )}
                    {models.map((model) => (
                      <option key={model} value={model}>
                        {model}
                      </option>
                    ))}
                  </select>
                ) : (
                  <>
                    <Input
                      id={`${formId}-target`}
                      required
                      list={`${formId}-models`}
                      {...form.register('target_model')}
                    />
                    <datalist id={`${formId}-models`}>
                      {models.map((model) => (
                        <option key={model} value={model} />
                      ))}
                    </datalist>
                  </>
                )}
              </div>
            </div>
            {targetsQuery.isError && (
              <p role='alert' className='text-destructive text-sm'>
                {t('Failed to load routing targets')}
              </p>
            )}
            <div className='space-y-3'>
              <p className='text-sm font-medium'>
                {t('Conditions (all must match)')}
              </p>
              {conditions.fields.map((field, index) => (
                <div
                  key={field.id}
                  className='grid items-start gap-2 md:grid-cols-[1fr_1fr_1fr_auto]'
                >
                  <select
                    aria-label={t('Condition operator')}
                    className={selectClass}
                    {...form.register(`conditions.${index}.operator`)}
                  >
                    <option value='has_video'>
                      {t('Contains video input')}
                    </option>
                    <option value='equals'>{t('Field equals')}</option>
                    <option value='exists'>{t('Field exists')}</option>
                  </select>
                  {values.conditions[index]?.operator !== 'has_video' && (
                    <div>
                      <Input
                        aria-label={t('Request field path')}
                        placeholder='thinking.type'
                        {...form.register(`conditions.${index}.path`)}
                      />
                      {form.formState.errors.conditions?.[index]?.path && (
                        <p role='alert' className='text-destructive text-sm'>
                          {t('Enter a field path, such as thinking.type')}
                        </p>
                      )}
                    </div>
                  )}
                  {values.conditions[index]?.operator === 'equals' && (
                    <div>
                      <Input
                        aria-label={t('JSON comparison value')}
                        placeholder='"disabled"'
                        {...form.register(`conditions.${index}.valueText`)}
                      />
                      {form.formState.errors.conditions?.[index]?.valueText && (
                        <p role='alert' className='text-destructive text-sm'>
                          {t('Enter a JSON string, number, boolean or null')}
                        </p>
                      )}
                    </div>
                  )}
                  <Button
                    type='button'
                    variant='outline'
                    onClick={() => conditions.remove(index)}
                    aria-label={t('Remove condition')}
                  >
                    <Trash2 />
                  </Button>
                </div>
              ))}
              <Button
                type='button'
                variant='outline'
                disabled={conditions.fields.length >= 8}
                onClick={() =>
                  conditions.append({
                    operator: 'has_video',
                    path: '',
                    valueText: '',
                  })
                }
              >
                <Plus />
                {t('Add condition')}
              </Button>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'No conditions means always match. Field paths use dots and array indices; values preserve JSON types. Video detection checks structured media in the full request history, not links in text.'
                )}
              </p>
            </div>
            <div className='flex flex-wrap gap-4'>
              <label className='flex items-center gap-2 text-sm'>
                <input
                  type='checkbox'
                  {...form.register('only_thinking_disabled')}
                />
                {t('Only when thinking is disabled')}
              </label>
              <label className='flex items-center gap-2 text-sm'>
                <input type='checkbox' {...form.register('seamless')} />
                {t('Keep the requested model visible')}
              </label>
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Switching models uses the target model for billing. Switching only channels keeps the same model pricing. Legacy thinking-disabled rules also remove the thinking parameter.'
              )}
            </p>
            {Object.keys(form.formState.errors).length > 0 && (
              <p role='alert' className='text-destructive text-sm'>
                {t('Check the routing rule fields')}
              </p>
            )}
            <div className='flex gap-2'>
              <Button type='submit'>
                {saveMutation.isPending ? <Spinner /> : <Plus />}
                {editing ? t('Save') : t('Add rule')}
              </Button>
              {editing && (
                <Button type='button' variant='outline' onClick={resetForm}>
                  {t('Cancel')}
                </Button>
              )}
            </div>
          </fieldset>
        </form>
        {rulesQuery.isLoading && <Spinner />}
        {rulesQuery.isError && (
          <p role='alert' className='text-destructive'>
            {t('Request failed')}
          </p>
        )}
        {!rulesQuery.isLoading && !rulesQuery.isError && rules.length === 0 && (
          <p className='text-muted-foreground'>{t('No routing rules')}</p>
        )}
        {rules.length > 0 && (
          <div className='overflow-x-auto'>
            <Table className='min-w-[900px]'>
              <TableHeader>
                <TableRow>
                  {[
                    t('User'),
                    t('Source model'),
                    t('Target model'),
                    t('Priority'),
                    t('Conditions'),
                    t('Actions'),
                  ].map((label) => (
                    <TableHead key={label}>{label}</TableHead>
                  ))}
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule) => (
                  <TableRow key={rule.id}>
                    <TableCell>
                      {rule.all_users
                        ? t('All users')
                        : `${rule.username || ''} #${rule.user_id}`}
                      {rule.disabled && (
                        <p className='text-muted-foreground'>{t('Disabled')}</p>
                      )}
                    </TableCell>
                    <TableCell>
                      {rule.source_model}
                      <p className='text-muted-foreground text-xs'>
                        {rule.endpoint || t('All endpoints')}
                      </p>
                    </TableCell>
                    <TableCell>
                      {rule.target_model}
                      <p className='text-muted-foreground text-xs'>
                        {rule.target_channel_id
                          ? `#${rule.target_channel_id} ${targets.find((channel) => channel.id === rule.target_channel_id)?.name || ''}`
                          : t('Automatic channel selection')}
                      </p>
                    </TableCell>
                    <TableCell>{rule.priority}</TableCell>
                    <TableCell>
                      {rule.only_thinking_disabled && (
                        <p>{t('Thinking disabled')}</p>
                      )}
                      <p className='whitespace-pre-line'>
                        {rule.conditions
                          ?.map((condition) => {
                            if (condition.operator === 'exists') {
                              return `${condition.path}: ${t('Field exists')}`
                            }
                            if (condition.operator === 'equals') {
                              return `${condition.path} = ${JSON.stringify(condition.value)}`
                            }
                            return t('Contains video input')
                          })
                          .join('\n')}
                      </p>
                      {!rule.only_thinking_disabled &&
                        !rule.conditions?.length &&
                        t('Always')}
                    </TableCell>
                    <TableCell>
                      <div className='flex gap-1'>
                        <Button
                          size='icon'
                          variant='ghost'
                          aria-label={t('Edit')}
                          onClick={() => {
                            setEditing(rule)
                            form.reset({
                              ...defaults,
                              ...rule,
                              sso_sub: '',
                              conditions: (rule.conditions ?? []).map(
                                (condition) => ({
                                  operator: condition.operator,
                                  path: condition.path ?? '',
                                  valueText:
                                    condition.operator === 'equals'
                                      ? JSON.stringify(condition.value)
                                      : '',
                                })
                              ),
                            })
                          }}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          size='icon'
                          variant='ghost'
                          aria-label={t('Delete')}
                          onClick={() => setDeleteTarget(rule)}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </SettingsCard>
      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete routing rule?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Matching requests will use the remaining rules or normal channel selection.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() =>
                deleteTarget && deleteMutation.mutate(deleteTarget)
              }
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
