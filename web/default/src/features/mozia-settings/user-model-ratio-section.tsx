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
  deleteMoziaUserModelRatio,
  getMoziaUserModelRatios,
  saveMoziaUserModelRatio,
} from './api'
import { UserModelRatioDialog } from './components/user-model-ratio-dialog'
import type { UserModelRatioFormValues } from './lib/user-model-ratio-schema'
import type { MoziaUserModelRatio } from './types'

const userModelRatioQueryKey = ['mozia', 'user-model-ratios'] as const
const emptyFormValues: UserModelRatioFormValues = {
  user_id: 0,
  model: '',
  ratio: 1,
}

function getErrorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

function formatPercentage(ratio: number) {
  return `${Number((ratio * 100).toFixed(6))}%`
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

  const ratiosQuery = useQuery({
    queryKey: userModelRatioQueryKey,
    queryFn: getMoziaUserModelRatios,
  })

  const sortedRules = useMemo(() => {
    return [...(ratiosQuery.data ?? [])].sort((left, right) => {
      if (left.user_id !== right.user_id) return left.user_id - right.user_id
      return left.model.localeCompare(right.model)
    })
  }, [ratiosQuery.data])

  const saveMutation = useMutation({
    mutationFn: saveMoziaUserModelRatio,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: userModelRatioQueryKey })
      toast.success(t('User model ratio saved'))
      setDialogOpen(false)
      setEditingRule(null)
    },
    onError: (error: unknown) => {
      toast.error(getErrorMessage(error, t('Failed to save user model ratio')))
    },
  })

  const deleteMutation = useMutation({
    mutationFn: deleteMoziaUserModelRatio,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: userModelRatioQueryKey })
      toast.success(t('User model ratio deleted'))
      setDeleteTarget(null)
    },
    onError: (error: unknown) => {
      toast.error(
        getErrorMessage(error, t('Failed to delete user model ratio'))
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
      await saveMutation.mutateAsync({
        user_id: values.user_id,
        model: values.model.trim(),
        ratio: values.ratio,
      })
    } catch {
      // The mutation error handler owns the user-facing error state.
    }
  }

  const dialogDefaultValues = editingRule ?? emptyFormValues

  let rulesContent = (
    <div className='text-muted-foreground flex min-h-36 items-center justify-center gap-2 text-sm'>
      <Spinner />
      {t('Loading user model ratios...')}
    </div>
  )

  if (ratiosQuery.isError) {
    rulesContent = (
      <Alert variant='destructive'>
        <AlertTitle>{t('Failed to load user model ratios')}</AlertTitle>
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
          <EmptyTitle>{t('No user model ratios')}</EmptyTitle>
          <EmptyDescription>
            {t(
              'Add a rule to override billing for an exact user and model combination.'
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
        <Table className='min-w-[640px]'>
          <TableHeader>
            <TableRow className='bg-muted/40 hover:bg-muted/40'>
              <TableHead className='px-4'>{t('User ID')}</TableHead>
              <TableHead>{t('Model')}</TableHead>
              <TableHead>{t('Billing multiplier')}</TableHead>
              <TableHead className='pr-4 text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sortedRules.map((rule) => (
              <TableRow key={`${rule.user_id}:${rule.model}`}>
                <TableCell className='px-4 font-medium'>
                  {rule.user_id}
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {rule.model}
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
          </TableBody>
        </Table>
      </div>
    )
  }

  return (
    <SettingsSection title={t('User Model Ratios')}>
      <SettingsCard
        title={t('User model ratio rules')}
        description={t(
          'The multiplier is applied after the existing model and group pricing is calculated.'
        )}
      >
        <Alert className='mb-3'>
          <AlertTitle>{t('Exact user and model match')}</AlertTitle>
          <AlertDescription>
            {t(
              'Model names are case-sensitive. A multiplier of 0.36 charges 36% of the otherwise calculated amount.'
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

        {rulesContent}
      </SettingsCard>

      {dialogOpen ? (
        <UserModelRatioDialog
          key={
            editingRule
              ? `${editingRule.user_id}:${editingRule.model}`
              : 'create'
          }
          open
          editing={editingRule !== null}
          pending={saveMutation.isPending}
          defaultValues={dialogDefaultValues}
          onOpenChange={(open) => {
            setDialogOpen(open)
            if (!open) setEditingRule(null)
          }}
          onSubmit={submitRule}
        />
      ) : null}

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete user model ratio?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteTarget
                ? t(
                    'The direct billing ratio for user {{userId}} and model {{model}} will be removed.',
                    {
                      userId: deleteTarget.user_id,
                      model: deleteTarget.model,
                    }
                  )
                : null}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending || deleteTarget === null}
              onClick={() => {
                if (deleteTarget) deleteMutation.mutate(deleteTarget)
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
