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
import { Plus, Trash2 } from 'lucide-react'
import { type FormEvent, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

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
  getMoziaUserModelRedirects,
  saveMoziaUserModelRedirect,
} from './api'
import type { MoziaUserModelRedirect } from './types'

const queryKey = ['mozia', 'user-model-redirects'] as const
const sourceModel = 'moonshotai/kimi-k3'
const targetModel = 'moonshotai/kimi-k2.6'

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

export function MoziaUserModelRedirectSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const formRef = useRef<HTMLFormElement>(null)
  const [deleteTarget, setDeleteTarget] =
    useState<MoziaUserModelRedirect | null>(null)

  const rulesQuery = useQuery({
    queryKey,
    queryFn: getMoziaUserModelRedirects,
  })
  const saveMutation = useMutation({
    mutationFn: saveMoziaUserModelRedirect,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey })
      formRef.current?.reset()
      toast.success(t('User model redirect saved'))
    },
    onError: (error: unknown) =>
      toast.error(errorMessage(error, t('Failed to save user model redirect'))),
  })
  const deleteMutation = useMutation({
    mutationFn: deleteMoziaUserModelRedirect,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey })
      toast.success(t('User model redirect deleted'))
      setDeleteTarget(null)
    },
    onError: (error: unknown) =>
      toast.error(
        errorMessage(error, t('Failed to delete user model redirect'))
      ),
  })

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const ssoSub = String(
      new FormData(event.currentTarget).get('sso_sub') ?? ''
    ).trim()
    if (!ssoSub) return
    saveMutation.mutate(ssoSub)
  }

  const rules = rulesQuery.data ?? []

  return (
    <SettingsSection title={t('User Model Redirects')}>
      <SettingsCard
        title={t('Thinking-disabled model redirects')}
        description={t(
          'For selected users, requests with thinking.type set to disabled use the configured target model.'
        )}
      >
        <form
          ref={formRef}
          className='mb-4 flex flex-col gap-2 sm:flex-row'
          onSubmit={submit}
        >
          <Input
            name='sso_sub'
            aria-label={t('SSO subject')}
            placeholder={t('SSO subject')}
            disabled={saveMutation.isPending}
            required
          />
          <Button type='submit' disabled={saveMutation.isPending}>
            {saveMutation.isPending ? (
              <Spinner data-icon='inline-start' />
            ) : (
              <Plus data-icon='inline-start' />
            )}
            {t('Add rule')}
          </Button>
        </form>

        {rulesQuery.isLoading ? (
          <div className='text-muted-foreground flex min-h-32 items-center justify-center gap-2 text-sm'>
            <Spinner /> {t('Loading user model redirects...')}
          </div>
        ) : rulesQuery.isError ? (
          <div className='text-destructive py-8 text-center text-sm'>
            {errorMessage(rulesQuery.error, t('Request failed'))}
          </div>
        ) : rules.length === 0 ? (
          <div className='text-muted-foreground py-8 text-center text-sm'>
            {t('No user model redirects')}
          </div>
        ) : (
          <div className='overflow-x-auto'>
            <Table className='min-w-[680px]'>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Source model')}</TableHead>
                  <TableHead>{t('Target model')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule) => (
                  <TableRow key={rule.user_id}>
                    <TableCell>
                      <div className='font-medium'>{rule.username || '-'}</div>
                      <div className='text-muted-foreground text-xs'>
                        #{rule.user_id}
                      </div>
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {sourceModel}
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {targetModel}
                    </TableCell>
                    <TableCell className='text-right'>
                      <Button
                        size='icon'
                        variant='ghost'
                        title={t('Delete')}
                        onClick={() => setDeleteTarget(rule)}
                      >
                        <Trash2 />
                      </Button>
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
            <AlertDialogTitle>{t('Delete redirect rule?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This user will stop using the configured thinking-disabled model redirect.'
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
