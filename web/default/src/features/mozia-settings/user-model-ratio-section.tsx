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
import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
import { UserInfoDialog } from '@/features/usage-logs/components/dialogs/user-info-dialog'

import {
  deleteMoziaUserModelRatio,
  getMoziaUserModelRatios,
  saveMoziaUserModelRatio,
} from './api'
import { UserModelRatioDialog } from './components/user-model-ratio-dialog'
import type { UserModelRatioFormValues } from './lib/user-model-ratio-schema'
import type { MoziaUserModelRatio, MoziaUserRatioScope } from './types'

const userModelRatioQueryKey = ['mozia', 'user-model-ratios'] as const
const emptyFormValues: UserModelRatioFormValues = {
  user_id: 0,
  scope: 'model',
  model: '',
  channel_id: undefined,
  ratio: 1,
}

function getErrorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

function formatPercentage(ratio: number) {
  return `${Number((ratio * 100).toFixed(6))}%`
}

function getRuleTarget(rule: MoziaUserModelRatio) {
  return rule.scope === 'model' ? rule.model : String(rule.channel_id)
}

export function MoziaUserModelRatioSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<MoziaUserModelRatio | null>(
    null
  )
  const [deleteTarget, setDeleteTarget] = useState<MoziaUserModelRatio | null>(
    null
  )
  const [selectedUserId, setSelectedUserId] = useState<number | null>(null)
  const [userInfoDialogOpen, setUserInfoDialogOpen] = useState(false)
  const [keywordFilter, setKeywordFilter] = useState('')
  const [userIdFilter, setUserIdFilter] = useState('')
  const [scopeFilter, setScopeFilter] = useState<MoziaUserRatioScope | 'all'>(
    'all'
  )

  const ratiosQuery = useQuery({
    queryKey: userModelRatioQueryKey,
    queryFn: getMoziaUserModelRatios,
  })

  const sortedRules = useMemo(() => {
    return [...(ratiosQuery.data ?? [])].sort((left, right) => {
      if (left.user_id !== right.user_id) {
        return left.user_id - right.user_id
      }
      if (left.scope !== right.scope) {
        return left.scope.localeCompare(right.scope)
      }
      return getRuleTarget(left).localeCompare(
        getRuleTarget(right),
        undefined,
        {
          numeric: true,
        }
      )
    })
  }, [ratiosQuery.data])

  const filteredRules = useMemo(() => {
    const keyword = keywordFilter.trim().toLowerCase()
    const userId = userIdFilter.trim()

    return sortedRules.filter((rule) => {
      if (scopeFilter !== 'all' && rule.scope !== scopeFilter) {
        return false
      }
      if (userId && !String(rule.user_id).includes(userId)) {
        return false
      }
      if (!keyword) {
        return true
      }

      const searchableTarget =
        rule.scope === 'model'
          ? rule.model.toLowerCase()
          : String(rule.channel_id)
      return (
        searchableTarget.includes(keyword) ||
        rule.username?.toLowerCase().includes(keyword) === true
      )
    })
  }, [keywordFilter, scopeFilter, sortedRules, userIdFilter])

  const saveMutation = useMutation({
    mutationFn: saveMoziaUserModelRatio,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: userModelRatioQueryKey })
      toast.success(t('User billing ratio saved'))
      setDialogOpen(false)
      setEditingRule(null)
    },
    onError: (error: unknown) => {
      toast.error(
        getErrorMessage(error, t('Failed to save user billing ratio'))
      )
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteMoziaUserModelRatio,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: userModelRatioQueryKey })
      toast.success(t('User billing ratio deleted'))
      setDeleteTarget(null)
    },
    onError: (error: unknown) => {
      toast.error(
        getErrorMessage(error, t('Failed to delete user billing ratio'))
      )
    },
  })

  const openCreateDialog = () => {
    setEditingRule(null)
    setDialogOpen(true)
  }

  const openEditDialog = (rule: MoziaUserModelRatio) => {
    setEditingRule(rule)
    setDialogOpen(true)
  }

  const submitRule = async (values: UserModelRatioFormValues) => {
    try {
      if (values.scope === 'model') {
        await saveMutation.mutateAsync({
          user_id: values.user_id,
          scope: 'model',
          model: values.model?.trim() ?? '',
          ratio: values.ratio,
        })
      } else if (values.channel_id !== undefined) {
        await saveMutation.mutateAsync({
          user_id: values.user_id,
          scope: 'channel',
          channel_id: values.channel_id,
          ratio: values.ratio,
        })
      }
    } catch {
      // The mutation error handler owns the user-facing error state.
    }
  }

  const dialogDefaultValues = editingRule ?? emptyFormValues
  let deleteDescription: string | null = null

  if (deleteTarget?.scope === 'model') {
    deleteDescription = t(
      'The direct billing ratio for user {{userId}} and model {{model}} will be removed.',
      {
        userId: deleteTarget.user_id,
        model: deleteTarget.model,
      }
    )
  } else if (deleteTarget?.scope === 'channel') {
    deleteDescription = t(
      'The direct billing ratio for user {{userId}} and channel {{channelId}} will be removed.',
      {
        userId: deleteTarget.user_id,
        channelId: deleteTarget.channel_id,
      }
    )
  }

  let rulesContent = (
    <div className='text-muted-foreground flex min-h-36 items-center justify-center gap-2 text-sm'>
      <Spinner />
      {t('Loading user billing ratios...')}
    </div>
  )

  if (ratiosQuery.isError) {
    rulesContent = (
      <Alert variant='destructive'>
        <AlertTitle>{t('Failed to load user billing ratios')}</AlertTitle>
        <AlertDescription className='grid gap-3'>
          <span>{getErrorMessage(ratiosQuery.error, t('Request failed'))}</span>
          <Button
            className='w-fit'
            variant='outline'
            size='sm'
            onClick={() => void ratiosQuery.refetch()}
          >
            {t('Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  } else if (!ratiosQuery.isLoading && sortedRules.length === 0) {
    rulesContent = (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{t('No user billing ratios')}</EmptyTitle>
          <EmptyDescription>
            {t(
              'Add a rule to override billing for one user and an exact model or channel.'
            )}
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button onClick={openCreateDialog}>{t('Add rule')}</Button>
        </EmptyContent>
      </Empty>
    )
  } else if (!ratiosQuery.isLoading) {
    rulesContent = (
      <div className='overflow-x-auto'>
        <Table className='min-w-[800px]'>
          <TableHeader>
            <TableRow className='bg-muted/40 hover:bg-muted/40'>
              <TableHead className='px-4'>{t('User ID')}</TableHead>
              <TableHead>{t('Username')}</TableHead>
              <TableHead>{t('Scope')}</TableHead>
              <TableHead>{t('Target')}</TableHead>
              <TableHead>{t('Billing multiplier')}</TableHead>
              <TableHead className='pr-4 text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {filteredRules.map((rule) => (
              <TableRow
                key={`${rule.user_id}:${rule.scope}:${getRuleTarget(rule)}`}
              >
                <TableCell className='px-4 font-medium'>
                  {rule.user_id}
                </TableCell>
                <TableCell>
                  {rule.username ? (
                    <button
                      type='button'
                      className='font-medium hover:underline'
                      onClick={() => {
                        setSelectedUserId(rule.user_id)
                        setUserInfoDialogOpen(true)
                      }}
                    >
                      {rule.username}
                    </button>
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </TableCell>
                <TableCell>
                  <Badge variant='outline'>
                    {rule.scope === 'model' ? t('Model') : t('Channel')}
                  </Badge>
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {rule.scope === 'model'
                    ? rule.model
                    : t('Channel #{{channelId}}', {
                        channelId: rule.channel_id,
                      })}
                </TableCell>
                <TableCell>
                  <Badge variant={rule.ratio < 1 ? 'default' : 'secondary'}>
                    {rule.ratio} ({formatPercentage(rule.ratio)})
                  </Badge>
                </TableCell>
                <TableCell className='pr-4'>
                  <div className='flex justify-end gap-2'>
                    <Button
                      variant='outline'
                      size='sm'
                      onClick={() => openEditDialog(rule)}
                    >
                      {t('Edit')}
                    </Button>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => setDeleteTarget(rule)}
                    >
                      {t('Delete')}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {filteredRules.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className='text-muted-foreground h-24 text-center'
                >
                  {t('No matching ratio rules')}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>
      </div>
    )
  }

  return (
    <SettingsSection title={t('User Billing Ratios')}>
      <SettingsCard
        title={t('User billing ratio rules')}
        description={t(
          'The multiplier is applied after the existing model and group pricing is calculated.'
        )}
      >
        <Alert className='mb-3'>
          <AlertTitle>{t('Model and channel scopes')}</AlertTitle>
          <AlertDescription>
            {t(
              'Model rules take priority over channel rules. Model names are case-sensitive, and a multiplier of 0.36 charges 36% of the otherwise calculated amount.'
            )}
          </AlertDescription>
        </Alert>
        <div className='mb-3 flex flex-wrap items-center justify-between gap-2'>
          <Button
            variant='outline'
            disabled={ratiosQuery.isFetching}
            onClick={() => void ratiosQuery.refetch()}
          >
            {ratiosQuery.isFetching ? (
              <Spinner data-icon='inline-start' />
            ) : null}
            {t('Refresh')}
          </Button>
          <Button onClick={openCreateDialog}>{t('Add rule')}</Button>
        </div>

        {!ratiosQuery.isLoading &&
        !ratiosQuery.isError &&
        sortedRules.length > 0 ? (
          <div className='mb-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-[minmax(12rem,1fr)_12rem_12rem]'>
            <InputGroup>
              <InputGroupAddon>
                <Search />
              </InputGroupAddon>
              <InputGroupInput
                value={keywordFilter}
                placeholder={`${t('Username')} / ${t('Model')} / ${t('Channel ID')}`}
                onChange={(event) => setKeywordFilter(event.target.value)}
              />
            </InputGroup>
            <Select<MoziaUserRatioScope | 'all'>
              items={[
                { value: 'all', label: t('All scopes') },
                { value: 'model', label: t('Model') },
                { value: 'channel', label: t('Channel') },
              ]}
              value={scopeFilter}
              onValueChange={(value) => {
                if (value !== null) {
                  setScopeFilter(value)
                }
              }}
            >
              <SelectTrigger className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectGroup>
                  <SelectItem value='all'>{t('All scopes')}</SelectItem>
                  <SelectItem value='model'>{t('Model')}</SelectItem>
                  <SelectItem value='channel'>{t('Channel')}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
            <Input
              type='number'
              min={1}
              step={1}
              value={userIdFilter}
              placeholder={t('Filter by user ID')}
              onChange={(event) => setUserIdFilter(event.target.value)}
            />
          </div>
        ) : null}

        {rulesContent}
      </SettingsCard>

      {dialogOpen ? (
        <UserModelRatioDialog
          key={
            editingRule
              ? `${editingRule.user_id}:${editingRule.scope}:${getRuleTarget(editingRule)}`
              : 'create'
          }
          open
          editing={editingRule !== null}
          pending={saveMutation.isPending}
          defaultValues={dialogDefaultValues}
          onOpenChange={(open) => {
            setDialogOpen(open)
            if (!open) {
              setEditingRule(null)
            }
          }}
          onSubmit={submitRule}
        />
      ) : null}

      <UserInfoDialog
        userId={selectedUserId}
        open={userInfoDialogOpen}
        onOpenChange={setUserInfoDialogOpen}
      />

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null)
          }
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Delete user billing ratio?')}
            </AlertDialogTitle>
            <AlertDialogDescription>{deleteDescription}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending || deleteTarget === null}
              onClick={() => {
                if (deleteTarget) {
                  deleteMutation.mutate(deleteTarget)
                }
              }}
            >
              {deleteMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
