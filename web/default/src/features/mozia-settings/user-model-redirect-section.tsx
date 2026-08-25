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
import { useState } from 'react'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { SettingsCard } from '@/features/system-settings/components/settings-card'
import { SettingsSection } from '@/features/system-settings/components/settings-section'

import {
  deleteMoziaUserModelRedirect,
  getMoziaUserModelRedirects,
  saveMoziaUserModelRedirect,
} from './api'
import type {
  MoziaUserModelRedirect,
  MoziaUserModelRedirectPayload,
} from './types'

const queryKey = ['mozia', 'user-model-redirects'] as const
const defaultSourceModel = 'moonshotai/kimi-k3'
const defaultTargetModel = 'moonshotai/kimi-k2.6'

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback
}

export function MoziaUserModelRedirectSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<MoziaUserModelRedirect | null>(null)
  const [deleteTarget, setDeleteTarget] =
    useState<MoziaUserModelRedirect | null>(null)
  const [ssoSub, setSsoSub] = useState('')
  const [sourceModel, setSourceModel] = useState(defaultSourceModel)
  const [targetModel, setTargetModel] = useState(defaultTargetModel)
  const [enabled, setEnabled] = useState(true)

  const rulesQuery = useQuery({
    queryKey,
    queryFn: getMoziaUserModelRedirects,
  })
  const saveMutation = useMutation({
    mutationFn: saveMoziaUserModelRedirect,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey })
      toast.success(t('User model redirect saved'))
      setDialogOpen(false)
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

  const openCreate = () => {
    setEditing(null)
    setSsoSub('')
    setSourceModel(defaultSourceModel)
    setTargetModel(defaultTargetModel)
    setEnabled(true)
    setDialogOpen(true)
  }
  const openEdit = (rule: MoziaUserModelRedirect) => {
    setEditing(rule)
    setSsoSub('')
    setSourceModel(rule.source_model)
    setTargetModel(rule.target_model)
    setEnabled(rule.enabled)
    setDialogOpen(true)
  }
  const submit = () => {
    const payload: MoziaUserModelRedirectPayload = {
      user_id: editing?.user_id ?? 0,
      source_model: sourceModel.trim(),
      target_model: targetModel.trim(),
      enabled,
    }
    if (!editing) payload.sso_sub = ssoSub.trim()
    if (
      (!editing && !payload.sso_sub) ||
      !payload.source_model ||
      !payload.target_model
    ) {
      toast.error(t('Complete all required fields'))
      return
    }
    saveMutation.mutate(payload)
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
        <div className='mb-3 flex justify-end'>
          <Button onClick={openCreate}>
            <Plus data-icon='inline-start' />
            {t('Add rule')}
          </Button>
        </div>

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
            <Table className='min-w-[760px]'>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Source model')}</TableHead>
                  <TableHead>{t('Target model')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule) => (
                  <TableRow key={`${rule.user_id}:${rule.source_model}`}>
                    <TableCell>
                      <div className='font-medium'>{rule.username || '-'}</div>
                      <div className='text-muted-foreground text-xs'>
                        #{rule.user_id}
                      </div>
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {rule.source_model}
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {rule.target_model}
                    </TableCell>
                    <TableCell>
                      <Badge variant={rule.enabled ? 'default' : 'secondary'}>
                        {rule.enabled ? t('Enabled') : t('Disabled')}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className='flex justify-end gap-1'>
                        <Button
                          size='icon'
                          variant='ghost'
                          title={t('Edit')}
                          onClick={() => openEdit(rule)}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          size='icon'
                          variant='ghost'
                          title={t('Delete')}
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

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>
              {editing ? t('Edit redirect rule') : t('Add redirect rule')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'The SSO subject is resolved to an internal user ID and is not stored in settings.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-4'>
            {editing ? (
              <div className='grid gap-2'>
                <Label>{t('User')}</Label>
                <Input
                  value={`${editing.username || '-'} (#${editing.user_id})`}
                  disabled
                />
              </div>
            ) : (
              <div className='grid gap-2'>
                <Label htmlFor='redirect-sso-sub'>{t('SSO subject')}</Label>
                <Input
                  id='redirect-sso-sub'
                  value={ssoSub}
                  onChange={(event) => setSsoSub(event.target.value)}
                  disabled={saveMutation.isPending}
                />
              </div>
            )}
            <div className='grid gap-2'>
              <Label htmlFor='redirect-source'>{t('Source model')}</Label>
              <Input
                id='redirect-source'
                value={sourceModel}
                onChange={(event) => setSourceModel(event.target.value)}
                disabled={editing !== null || saveMutation.isPending}
              />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='redirect-target'>{t('Target model')}</Label>
              <Input
                id='redirect-target'
                value={targetModel}
                onChange={(event) => setTargetModel(event.target.value)}
                disabled={saveMutation.isPending}
              />
            </div>
            <div className='flex items-center justify-between gap-4'>
              <Label htmlFor='redirect-enabled'>{t('Enabled')}</Label>
              <Switch
                id='redirect-enabled'
                checked={enabled}
                onCheckedChange={setEnabled}
                disabled={saveMutation.isPending}
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setDialogOpen(false)}
              disabled={saveMutation.isPending}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={submit} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? (
                <Spinner data-icon='inline-start' />
              ) : null}
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
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
