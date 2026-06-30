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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { api } from '@/lib/api'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SettingsCard } from '../components/settings-card'
import { SettingsSection } from '../components/settings-section'

type QuotaSource = 'gift' | 'paid' | 'legacy'
type MatchType = 'exact' | 'prefix' | 'wildcard'
type ConsumeOrder = 'gift_first' | 'paid_first'

type MoziaQuotaPolicy = {
  id: number
  model_pattern: string
  match_type: MatchType
  allowed_sources: string
  consume_order: ConsumeOrder
  enabled: boolean
  priority: number
  created_time?: number
  updated_time?: number
}

type ApiEnvelope<T> = {
  success: boolean
  message?: string
  data: T
}

type PolicyFormState = {
  id?: number
  model_pattern: string
  match_type: MatchType
  allowed_sources: QuotaSource[]
  consume_order: ConsumeOrder
  enabled: boolean
  priority: number
}

const sourceOptions: Array<{ value: QuotaSource; labelKey: string }> = [
  { value: 'gift', labelKey: 'Gift quota' },
  { value: 'paid', labelKey: 'Paid quota' },
  { value: 'legacy', labelKey: 'Legacy quota' },
]

const matchTypeOptions: Array<{ value: MatchType; labelKey: string }> = [
  { value: 'exact', labelKey: 'Exact match' },
  { value: 'prefix', labelKey: 'Prefix match' },
  { value: 'wildcard', labelKey: 'Wildcard match' },
]

const consumeOrderOptions: Array<{ value: ConsumeOrder; labelKey: string }> = [
  { value: 'gift_first', labelKey: 'Gift first' },
  { value: 'paid_first', labelKey: 'Paid first' },
]

const emptyPolicyForm: PolicyFormState = {
  model_pattern: '',
  match_type: 'exact',
  allowed_sources: ['gift', 'paid', 'legacy'],
  consume_order: 'gift_first',
  enabled: true,
  priority: 0,
}

function parseSources(value: string): QuotaSource[] {
  const allowed = new Set<QuotaSource>(['gift', 'paid', 'legacy'])
  const parsed = value
    .split(',')
    .map((item) => item.trim())
    .filter((item): item is QuotaSource => allowed.has(item as QuotaSource))
  return parsed.length > 0 ? parsed : ['gift', 'paid', 'legacy']
}

function policyToForm(policy: MoziaQuotaPolicy): PolicyFormState {
  return {
    id: policy.id,
    model_pattern: policy.model_pattern,
    match_type: policy.match_type,
    allowed_sources: parseSources(policy.allowed_sources),
    consume_order: policy.consume_order,
    enabled: policy.enabled,
    priority: policy.priority,
  }
}

function formToPayload(form: PolicyFormState) {
  return {
    model_pattern: form.model_pattern.trim(),
    match_type: form.match_type,
    allowed_sources: form.allowed_sources.join(','),
    consume_order: form.consume_order,
    enabled: form.enabled,
    priority: Number(form.priority) || 0,
  }
}

function formatSources(value: string) {
  return parseSources(value)
}

async function fetchPolicies() {
  const res = await api.get<ApiEnvelope<MoziaQuotaPolicy[]>>(
    '/api/mozia/quota-policy/'
  )
  return res.data
}

async function savePolicy(form: PolicyFormState) {
  const payload = formToPayload(form)
  if (form.id) {
    const res = await api.put<ApiEnvelope<MoziaQuotaPolicy>>(
      `/api/mozia/quota-policy/${form.id}`,
      payload
    )
    return res.data
  }
  const res = await api.post<ApiEnvelope<MoziaQuotaPolicy>>(
    '/api/mozia/quota-policy/',
    payload
  )
  return res.data
}

async function deletePolicy(id: number) {
  const res = await api.delete<ApiEnvelope<null>>(
    `/api/mozia/quota-policy/${id}`
  )
  return res.data
}

function sourceLabelKey(source: QuotaSource) {
  return sourceOptions.find((item) => item.value === source)?.labelKey ?? source
}

function SourceBadges({ sources }: { sources: QuotaSource[] }) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap gap-1.5'>
      {sources.map((source) => (
        <Badge
          key={source}
          variant={source === 'paid' ? 'default' : 'secondary'}
        >
          {t(sourceLabelKey(source))}
        </Badge>
      ))}
    </div>
  )
}

type PolicyDialogProps = {
  open: boolean
  form: PolicyFormState
  saving: boolean
  onOpenChange: (open: boolean) => void
  onFormChange: (form: PolicyFormState) => void
  onSubmit: () => void
}

function PolicyDialog(props: PolicyDialogProps) {
  const { t } = useTranslation()
  const isEditing = Boolean(props.form.id)

  const setSource = (source: QuotaSource, checked: boolean) => {
    const next = checked
      ? [...new Set([...props.form.allowed_sources, source])]
      : props.form.allowed_sources.filter((item) => item !== source)
    props.onFormChange({
      ...props.form,
      allowed_sources: next,
    })
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>
            {isEditing ? t('Edit quota policy') : t('Create quota policy')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Policies restrict which wallet quota sources can be used by matched models.'
            )}
          </DialogDescription>
        </DialogHeader>

        <FieldGroup className='gap-4'>
          <Field>
            <FieldLabel htmlFor='mozia-policy-pattern'>
              {t('Model pattern')}
            </FieldLabel>
            <Input
              id='mozia-policy-pattern'
              value={props.form.model_pattern}
              placeholder='gpt-4o-mini'
              onChange={(event) =>
                props.onFormChange({
                  ...props.form,
                  model_pattern: event.target.value,
                })
              }
            />
            <FieldDescription>
              {t('Use a literal model name, prefix, or wildcard pattern.')}
            </FieldDescription>
          </Field>

          <div className='grid gap-4 md:grid-cols-2'>
            <Field>
              <FieldLabel htmlFor='mozia-policy-match-type'>
                {t('Match type')}
              </FieldLabel>
              <NativeSelect
                id='mozia-policy-match-type'
                value={props.form.match_type}
                onChange={(event) =>
                  props.onFormChange({
                    ...props.form,
                    match_type: event.target.value as MatchType,
                  })
                }
              >
                {matchTypeOptions.map((option) => (
                  <NativeSelectOption key={option.value} value={option.value}>
                    {t(option.labelKey)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>

            <Field>
              <FieldLabel htmlFor='mozia-policy-consume-order'>
                {t('Consume order')}
              </FieldLabel>
              <NativeSelect
                id='mozia-policy-consume-order'
                value={props.form.consume_order}
                onChange={(event) =>
                  props.onFormChange({
                    ...props.form,
                    consume_order: event.target.value as ConsumeOrder,
                  })
                }
              >
                {consumeOrderOptions.map((option) => (
                  <NativeSelectOption key={option.value} value={option.value}>
                    {t(option.labelKey)}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
          </div>

          <FieldSet>
            <FieldLegend variant='label'>{t('Allowed quota sources')}</FieldLegend>
            <FieldDescription>
              {t(
                'A request can use the model only when at least one selected source has available balance.'
              )}
            </FieldDescription>
            <FieldGroup data-slot='checkbox-group' className='gap-2'>
              {sourceOptions.map((source) => (
                <Field key={source.value} orientation='horizontal'>
                  <Checkbox
                    id={`mozia-policy-source-${source.value}`}
                    checked={props.form.allowed_sources.includes(source.value)}
                    onCheckedChange={(checked) =>
                      setSource(source.value, Boolean(checked))
                    }
                  />
                  <FieldContent>
                    <FieldLabel htmlFor={`mozia-policy-source-${source.value}`}>
                      {t(source.labelKey)}
                    </FieldLabel>
                  </FieldContent>
                </Field>
              ))}
            </FieldGroup>
          </FieldSet>

          <div className='grid gap-4 md:grid-cols-2'>
            <Field>
              <FieldLabel htmlFor='mozia-policy-priority'>
                {t('Priority')}
              </FieldLabel>
              <Input
                id='mozia-policy-priority'
                type='number'
                value={props.form.priority}
                onChange={(event) =>
                  props.onFormChange({
                    ...props.form,
                    priority: Number(event.target.value),
                  })
                }
              />
            </Field>

            <Field orientation='horizontal' className='items-center md:pt-6'>
              <Switch
                checked={props.form.enabled}
                onCheckedChange={(checked) =>
                  props.onFormChange({
                    ...props.form,
                    enabled: Boolean(checked),
                  })
                }
              />
              <FieldContent>
                <FieldLabel>{t('Enabled')}</FieldLabel>
              </FieldContent>
            </Field>
          </div>
        </FieldGroup>

        <DialogFooter>
          <Button
            variant='outline'
            type='button'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={props.saving}
            onClick={props.onSubmit}
          >
            {props.saving && <Spinner data-icon='inline-start' />}
            {isEditing ? t('Save changes') : t('Create policy')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function MoziaQuotaPolicySection() {
  const { t } = useTranslation()
  const [policies, setPolicies] = useState<MoziaQuotaPolicy[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [form, setForm] = useState<PolicyFormState>(emptyPolicyForm)
  const [deleteTarget, setDeleteTarget] = useState<MoziaQuotaPolicy | null>(
    null
  )

  const sortedPolicies = useMemo(
    () =>
      [...policies].sort((a, b) => {
        if (a.priority !== b.priority) return b.priority - a.priority
        return b.id - a.id
      }),
    [policies]
  )

  const loadPolicies = async () => {
    setLoading(true)
    try {
      const res = await fetchPolicies()
      if (res.success) {
        setPolicies(res.data ?? [])
      } else {
        toast.error(res.message || t('Request failed'))
      }
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void loadPolicies()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const openCreateDialog = () => {
    setForm(emptyPolicyForm)
    setDialogOpen(true)
  }

  const openEditDialog = (policy: MoziaQuotaPolicy) => {
    setForm(policyToForm(policy))
    setDialogOpen(true)
  }

  const submitPolicy = async () => {
    if (!form.model_pattern.trim()) {
      toast.error(t('Model pattern is required'))
      return
    }
    if (form.allowed_sources.length === 0) {
      toast.error(t('Select at least one quota source'))
      return
    }
    setSaving(true)
    try {
      const res = await savePolicy(form)
      if (res.success) {
        toast.success(t('Saved successfully'))
        setDialogOpen(false)
        await loadPolicies()
      } else {
        toast.error(res.message || t('Save failed'))
      }
    } finally {
      setSaving(false)
    }
  }

  const confirmDelete = async () => {
    if (!deleteTarget) return
    setSaving(true)
    try {
      const res = await deletePolicy(deleteTarget.id)
      if (res.success) {
        toast.success(t('Deleted successfully'))
        setDeleteTarget(null)
        await loadPolicies()
      } else {
        toast.error(res.message || t('Delete failed'))
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <SettingsSection title={t('Mozia Model Quota Policies')}>
      <SettingsCard
        title={t('Configuration notes')}
        description={t(
          'The built-in balance remains the total wallet mirror. These policies decide which source can be spent by each model.'
        )}
      >
        <div className='grid gap-4 text-sm lg:grid-cols-[minmax(0,1fr)_minmax(320px,0.8fr)]'>
          <div className='space-y-3 text-muted-foreground'>
            <p>
              {t(
                'If no policy matches a model, gift, paid, and legacy quota are all allowed.'
              )}
            </p>
            <p>
              {t(
                'Higher priority policies win first; ties prefer exact, then prefix, then wildcard matches.'
              )}
            </p>
            <p>
              {t(
                'Use paid-only policies for high-cost models to prevent registration gift abuse.'
              )}
            </p>
          </div>
          <div className='grid gap-3'>
            <Alert>
              <AlertTitle>{t('Example: paid-only premium models')}</AlertTitle>
              <AlertDescription>
                <code className='block whitespace-pre-wrap rounded-md bg-muted p-2 text-xs text-foreground'>
                  {`model_pattern: claude-*
match_type: wildcard
allowed_sources: paid
consume_order: paid_first
priority: 100`}
                </code>
              </AlertDescription>
            </Alert>
            <Alert>
              <AlertTitle>{t('Example: trial-friendly small model')}</AlertTitle>
              <AlertDescription>
                <code className='block whitespace-pre-wrap rounded-md bg-muted p-2 text-xs text-foreground'>
                  {`model_pattern: gpt-4o-mini
match_type: exact
allowed_sources: gift,paid,legacy
consume_order: gift_first
priority: 10`}
                </code>
              </AlertDescription>
            </Alert>
          </div>
        </div>
      </SettingsCard>

      <SettingsCard
        title={t('Quota policies')}
        description={t('Manage model access by wallet quota source.')}
      >
        <div className='mb-3 flex flex-wrap items-center justify-between gap-2'>
          <Button variant='outline' onClick={loadPolicies} disabled={loading}>
            {loading ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <RefreshCw data-icon='inline-start' />
            )}
            {t('Refresh')}
          </Button>
          <Button onClick={openCreateDialog}>
            <Plus data-icon='inline-start' />
            {t('Create policy')}
          </Button>
        </div>

        {loading ? (
          <div className='text-muted-foreground flex min-h-32 items-center justify-center gap-2 text-sm'>
            <Spinner />
            {t('Loading policies...')}
          </div>
        ) : sortedPolicies.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyTitle>{t('No quota policies')}</EmptyTitle>
              <EmptyDescription>
                {t(
                  'Create a policy when a model should require paid or restricted quota sources.'
                )}
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <Button onClick={openCreateDialog}>
                <Plus data-icon='inline-start' />
                {t('Create policy')}
              </Button>
            </EmptyContent>
          </Empty>
        ) : (
          <div className='overflow-x-auto'>
            <Table className='min-w-[900px]'>
              <TableHeader>
                <TableRow className='bg-muted/40 hover:bg-muted/40'>
                  <TableHead className='px-4'>{t('Model pattern')}</TableHead>
                  <TableHead>{t('Match type')}</TableHead>
                  <TableHead>{t('Allowed quota sources')}</TableHead>
                  <TableHead>{t('Consume order')}</TableHead>
                  <TableHead>{t('Priority')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead className='pr-4 text-right'>
                    {t('Actions')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedPolicies.map((policy) => (
                  <TableRow key={policy.id}>
                    <TableCell className='px-4 font-mono text-xs'>
                      {policy.model_pattern}
                    </TableCell>
                    <TableCell>{t(policy.match_type)}</TableCell>
                    <TableCell>
                      <SourceBadges
                        sources={formatSources(policy.allowed_sources)}
                      />
                    </TableCell>
                    <TableCell>{t(policy.consume_order)}</TableCell>
                    <TableCell>{policy.priority}</TableCell>
                    <TableCell>
                      <Badge variant={policy.enabled ? 'default' : 'secondary'}>
                        {policy.enabled ? t('Enabled') : t('Disabled')}
                      </Badge>
                    </TableCell>
                    <TableCell className='pr-4'>
                      <div className='flex justify-end gap-1'>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          title={t('Edit')}
                          onClick={() => openEditDialog(policy)}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          variant='ghost'
                          size='icon-sm'
                          title={t('Delete')}
                          onClick={() => setDeleteTarget(policy)}
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

      <PolicyDialog
        open={dialogOpen}
        form={form}
        saving={saving}
        onOpenChange={setDialogOpen}
        onFormChange={setForm}
        onSubmit={submitPolicy}
      />

      <AlertDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete quota policy?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('This policy will stop affecting model quota source checks.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={saving}
              onClick={confirmDelete}
            >
              {saving && <Spinner data-icon='inline-start' />}
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
