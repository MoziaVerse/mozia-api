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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ComboboxInput } from '@/components/ui/combobox-input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import { SettingsSection } from '../components/settings-section'
import {
  deleteChannelCost,
  getChannelCosts,
  saveChannelCost,
  type ChannelCostConfig,
  type ChannelCostMode,
  type ChannelCostRecord,
} from './channel-cost-pricing-api'
import {
  buildTaskBillingConfig,
  createTaskBillingDraft,
  parseTaskBillingDraft,
  validateTaskBillingDraft,
  type TaskBillingDraft,
} from './task-billing-config'
import { TaskBillingEditor } from './task-billing-editor'

const TOKEN_ITEMS = [
  'input',
  'output',
  'cache_read',
  'cache_write',
  'image_input',
  'audio_input',
  'audio_output',
] as const

const MODES: ChannelCostMode[] = [
  'per_token',
  'per_request',
  'per_second',
  'parametric',
  'tiered_expr',
]

type Draft = {
  id?: number
  channelId: string
  modelName: string
  currency: 'CNY' | 'USD'
  mode: ChannelCostMode
  prices: Record<string, string>
  basePrice: string
  referenceVideoPrice: string
  billingExpr: string
  taskBilling: TaskBillingDraft
  note: string
}

const emptyDraft = (): Draft => ({
  channelId: '',
  modelName: '',
  currency: 'CNY',
  mode: 'per_token',
  prices: {},
  basePrice: '',
  referenceVideoPrice: '',
  billingExpr: '',
  taskBilling: createTaskBillingDraft(),
  note: '',
})

const recordToDraft = (record: ChannelCostRecord): Draft => {
  const taskBilling = parseTaskBillingDraft(
    record.config.task_billing ? JSON.stringify(record.config.task_billing) : ''
  )
  if (record.mode === 'parametric') taskBilling.mode = 'parametric'
  return {
    id: record.id,
    channelId: String(record.channel_id),
    modelName: record.model_name,
    currency: record.currency,
    mode: record.mode,
    prices: Object.fromEntries(
      Object.entries(record.config.items ?? {}).map(([key, value]) => [
        key,
        String(value),
      ])
    ),
    basePrice:
      record.config.base_price === undefined
        ? ''
        : String(record.config.base_price),
    referenceVideoPrice:
      record.config.reference_video_price === undefined
        ? ''
        : String(record.config.reference_video_price),
    billingExpr: record.config.billing_expr ?? '',
    taskBilling,
    note: record.note,
  }
}

const modeLabel = (mode: ChannelCostMode) => {
  const labels: Record<ChannelCostMode, string> = {
    per_token: 'Per token',
    per_request: 'Per request',
    per_second: 'Per second',
    parametric: 'Parametric',
    tiered_expr: 'Tiered expression',
  }
  return labels[mode]
}

const parsePrice = (value: string) => {
  const parsed = Number(value)
  return value.trim() && Number.isFinite(parsed) && parsed >= 0 ? parsed : null
}

const buildConfig = (draft: Draft): ChannelCostConfig => {
  if (draft.mode === 'per_token') {
    return {
      items: Object.fromEntries(
        Object.entries(draft.prices)
          .map(([key, value]) => [key, parsePrice(value)] as const)
          .filter(
            (entry): entry is readonly [string, number] => entry[1] !== null
          )
      ),
    }
  }
  if (draft.mode === 'tiered_expr') {
    return { billing_expr: draft.billingExpr.trim() }
  }
  const basePrice = parsePrice(draft.basePrice)
  const referenceVideoPrice = parsePrice(draft.referenceVideoPrice)
  return {
    ...(basePrice === null ? {} : { base_price: basePrice }),
    ...(referenceVideoPrice === null
      ? {}
      : { reference_video_price: referenceVideoPrice }),
    ...(draft.mode === 'per_second' || draft.mode === 'parametric'
      ? { task_billing: buildTaskBillingConfig(draft.taskBilling) }
      : {}),
  }
}

export function ChannelCostPricingSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Draft | null>(null)
  const [search, setSearch] = useState('')
  const query = useQuery({
    queryKey: ['channel-cost-pricing'],
    queryFn: getChannelCosts,
  })
  const mutation = useMutation({
    mutationFn: saveChannelCost,
    onSuccess: () => {
      toast.success(t('Channel cost saved'))
      setDraft(null)
      queryClient.invalidateQueries({ queryKey: ['channel-cost-pricing'] })
    },
  })
  const deleteMutation = useMutation({
    mutationFn: deleteChannelCost,
    onSuccess: () => {
      toast.success(t('Channel cost deleted'))
      queryClient.invalidateQueries({ queryKey: ['channel-cost-pricing'] })
    },
  })

  const channelsById = useMemo(
    () =>
      new Map(
        (query.data?.channels ?? []).map((channel) => [channel.id, channel])
      ),
    [query.data?.channels]
  )
  const modelOptions = useMemo(() => {
    const models = new Set(query.data?.models ?? [])
    for (const channel of query.data?.channels ?? []) {
      channel.models
        .split(',')
        .map((model) => model.trim())
        .filter(Boolean)
        .forEach((model) => models.add(model))
    }
    return [...models].sort().map((model) => ({ value: model, label: model }))
  }, [query.data])
  const filteredItems = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return query.data?.items ?? []
    return (query.data?.items ?? []).filter((item) => {
      const channelName = channelsById.get(item.channel_id)?.name ?? ''
      return `${item.model_name} ${channelName}`.toLowerCase().includes(keyword)
    })
  }, [channelsById, query.data?.items, search])

  const openEditor = (record?: ChannelCostRecord) => {
    const next = record ? recordToDraft(record) : emptyDraft()
    setDraft(next)
  }

  const changeMode = (mode: ChannelCostMode) => {
    if (!draft) return
    const taskBilling = { ...draft.taskBilling }
    if (mode === 'per_second' || mode === 'parametric') taskBilling.mode = mode
    setDraft({ ...draft, mode, taskBilling })
  }

  const save = () => {
    if (!draft) return
    if (!draft.channelId || !draft.modelName.trim()) {
      toast.error(t('Channel and model are required'))
      return
    }
    if (draft.mode === 'per_token') {
      const prices = Object.values(draft.prices).filter((value) => value.trim())
      if (
        prices.length === 0 ||
        prices.some((value) => parsePrice(value) === null)
      ) {
        toast.error(t('Prices must be non-negative numbers.'))
        return
      }
    }
    if (
      draft.referenceVideoPrice.trim() &&
      parsePrice(draft.referenceVideoPrice) === null
    ) {
      toast.error(t('Prices must be non-negative numbers.'))
      return
    }
    if (draft.mode === 'per_second' || draft.mode === 'parametric') {
      const error = validateTaskBillingDraft(draft.taskBilling)
      if (error) {
        toast.error(t(error))
        return
      }
    }
    mutation.mutate({
      channel_id: Number(draft.channelId),
      model_name: draft.modelName.trim(),
      currency: draft.currency,
      mode: draft.mode,
      note: draft.note.trim(),
      config: buildConfig(draft),
    })
  }

  const currencySymbol = draft?.currency === 'USD' ? '$' : '¥'

  return (
    <SettingsSection title={t('Channel Cost References')}>
      <div className='text-muted-foreground text-sm'>
        {t(
          'Channel costs are internal pricing references only and never affect routing, billing, or settlement.'
        )}
      </div>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <Input
          className='sm:max-w-xs'
          value={search}
          placeholder={t('Search model or channel')}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Button type='button' onClick={() => openEditor()}>
          <Plus data-icon='inline-start' />
          {t('Add channel cost')}
        </Button>
      </div>
      <div className='rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Model')}</TableHead>
              <TableHead>{t('Channel')}</TableHead>
              <TableHead>{t('Pricing mode')}</TableHead>
              <TableHead>{t('Currency')}</TableHead>
              <TableHead className='w-24 text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredItems.map((item) => (
              <TableRow key={item.id}>
                <TableCell className='max-w-[320px] truncate font-medium'>
                  {item.model_name}
                </TableCell>
                <TableCell>
                  {channelsById.get(item.channel_id)?.name ??
                    `#${item.channel_id}`}
                </TableCell>
                <TableCell>
                  <Badge variant='secondary'>{t(modeLabel(item.mode))}</Badge>
                </TableCell>
                <TableCell>{item.currency}</TableCell>
                <TableCell>
                  <div className='flex justify-end gap-1'>
                    <Button
                      type='button'
                      size='icon-sm'
                      variant='ghost'
                      aria-label={t('Edit')}
                      onClick={() => openEditor(item)}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      type='button'
                      size='icon-sm'
                      variant='ghost'
                      aria-label={t('Delete')}
                      disabled={deleteMutation.isPending}
                      onClick={() => {
                        if (window.confirm(t('Delete this channel cost?'))) {
                          deleteMutation.mutate(item.id)
                        }
                      }}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {!query.isLoading && filteredItems.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className='text-muted-foreground py-10 text-center'
                >
                  {t('No channel costs configured')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <Dialog
        open={draft !== null}
        onOpenChange={(open) => !open && setDraft(null)}
      >
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-3xl'>
          <DialogHeader>
            <DialogTitle>
              {t(draft?.id ? 'Edit channel cost' : 'Add channel cost')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'This information is used only as an internal pricing reference.'
              )}
            </DialogDescription>
          </DialogHeader>
          {draft && (
            <div className='grid gap-5'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='grid min-w-0 gap-2'>
                  <Label htmlFor='channel-cost-channel'>{t('Channel')}</Label>
                  {draft.id !== undefined ? (
                    <Input
                      id='channel-cost-channel'
                      value={
                        channelsById.get(Number(draft.channelId))?.name ??
                        `#${draft.channelId}`
                      }
                      className='truncate'
                      disabled
                    />
                  ) : (
                    <ComboboxInput
                      id='channel-cost-channel'
                      options={(query.data?.channels ?? []).map((channel) => ({
                        value: String(channel.id),
                        label: `${channel.name} (#${channel.id})`,
                      }))}
                      value={draft.channelId}
                      className='truncate'
                      placeholder={t('Search or select a channel')}
                      emptyText={t('No channels found')}
                      onValueChange={(channelId) =>
                        setDraft({ ...draft, channelId })
                      }
                    />
                  )}
                </div>
                <div className='grid min-w-0 gap-2'>
                  <Label htmlFor='channel-cost-model'>
                    {t('Platform model')}
                  </Label>
                  {draft.id !== undefined ? (
                    <Input
                      id='channel-cost-model'
                      value={draft.modelName}
                      className='truncate'
                      disabled
                    />
                  ) : (
                    <ComboboxInput
                      id='channel-cost-model'
                      options={modelOptions}
                      value={draft.modelName}
                      className='truncate'
                      allowCustomValue
                      placeholder={t('Search or enter a model')}
                      onValueChange={(modelName) =>
                        setDraft({ ...draft, modelName })
                      }
                    />
                  )}
                </div>
              </div>
              <div className='grid gap-4 sm:grid-cols-2'>
                <div className='grid gap-2'>
                  <Label htmlFor='channel-cost-currency'>{t('Currency')}</Label>
                  <select
                    id='channel-cost-currency'
                    className='border-input bg-background h-9 rounded-md border px-3 text-sm'
                    value={draft.currency}
                    onChange={(event) =>
                      setDraft({
                        ...draft,
                        currency: event.target.value as 'CNY' | 'USD',
                      })
                    }
                  >
                    <option value='CNY'>CNY (¥)</option>
                    <option value='USD'>USD ($)</option>
                  </select>
                </div>
                <div className='grid gap-2'>
                  <Label htmlFor='channel-cost-note'>{t('Note')}</Label>
                  <Input
                    id='channel-cost-note'
                    value={draft.note}
                    placeholder={t('Optional source or contract note')}
                    onChange={(event) =>
                      setDraft({ ...draft, note: event.target.value })
                    }
                  />
                </div>
              </div>
              <Tabs
                value={draft.mode}
                onValueChange={(value) => changeMode(value as ChannelCostMode)}
              >
                <TabsList className='h-auto flex-wrap justify-start'>
                  {MODES.map((mode) => (
                    <TabsTrigger key={mode} value={mode}>
                      {t(modeLabel(mode))}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
              <div className='grid gap-4'>
                {draft.mode === 'per_token' && (
                  <div className='grid gap-4 sm:grid-cols-2'>
                    {TOKEN_ITEMS.map((item) => (
                      <div key={item} className='grid gap-2'>
                        <Label htmlFor={`channel-cost-${item}`}>
                          {t(item)}
                        </Label>
                        <div className='relative'>
                          <span className='text-muted-foreground absolute top-1/2 left-3 -translate-y-1/2'>
                            {currencySymbol}
                          </span>
                          <Input
                            id={`channel-cost-${item}`}
                            className='pl-8'
                            inputMode='decimal'
                            placeholder={t('per 1M tokens')}
                            value={draft.prices[item] ?? ''}
                            onChange={(event) =>
                              setDraft({
                                ...draft,
                                prices: {
                                  ...draft.prices,
                                  [item]: event.target.value,
                                },
                              })
                            }
                          />
                        </div>
                      </div>
                    ))}
                  </div>
                )}
                {(draft.mode === 'per_request' ||
                  draft.mode === 'per_second' ||
                  draft.mode === 'parametric') && (
                  <div className='grid gap-4 sm:grid-cols-2'>
                    <div className='grid gap-2'>
                      <Label htmlFor='channel-cost-base-price'>
                        {t('Base price')}
                      </Label>
                      <div className='relative'>
                        <span className='text-muted-foreground absolute top-1/2 left-3 -translate-y-1/2'>
                          {currencySymbol}
                        </span>
                        <Input
                          id='channel-cost-base-price'
                          className='pl-8'
                          inputMode='decimal'
                          value={draft.basePrice}
                          onChange={(event) =>
                            setDraft({
                              ...draft,
                              basePrice: event.target.value,
                            })
                          }
                        />
                      </div>
                    </div>
                    <div className='grid gap-2'>
                      <Label htmlFor='channel-cost-reference-video'>
                        {t('Reference video price')}
                      </Label>
                      <div className='relative'>
                        <span className='text-muted-foreground absolute top-1/2 left-3 -translate-y-1/2'>
                          {currencySymbol}
                        </span>
                        <Input
                          id='channel-cost-reference-video'
                          className='pl-8'
                          inputMode='decimal'
                          value={draft.referenceVideoPrice}
                          placeholder={t('Optional')}
                          onChange={(event) =>
                            setDraft({
                              ...draft,
                              referenceVideoPrice: event.target.value,
                            })
                          }
                        />
                      </div>
                    </div>
                  </div>
                )}
                {(draft.mode === 'per_second' ||
                  draft.mode === 'parametric') && (
                  <TaskBillingEditor
                    draft={draft.taskBilling}
                    onChange={(taskBilling) =>
                      setDraft({ ...draft, taskBilling })
                    }
                  />
                )}
                {draft.mode === 'tiered_expr' && (
                  <div className='grid gap-2'>
                    <Label htmlFor='channel-cost-expression'>
                      {t('Billing expression')}
                    </Label>
                    <Textarea
                      id='channel-cost-expression'
                      className='min-h-56 font-mono text-xs'
                      value={draft.billingExpr}
                      onChange={(event) =>
                        setDraft({
                          ...draft,
                          billingExpr: event.target.value,
                        })
                      }
                    />
                  </div>
                )}
              </div>
            </div>
          )}
          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => setDraft(null)}
            >
              {t('Cancel')}
            </Button>
            <Button type='button' disabled={mutation.isPending} onClick={save}>
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SettingsSection>
  )
}
