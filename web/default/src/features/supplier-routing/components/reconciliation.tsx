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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

import { reconcileSupplierAttempt, type SupplierAttempt } from '../api'

export function SupplierReconciliation(props: {
  attempt: SupplierAttempt
  onClose: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm({
    defaultValues: {
      status: 'failed',
      cost: '',
      currency: props.attempt.currency || 'CNY',
      note: '',
    },
  })
  const save = useMutation({
    mutationFn: (values: {
      status: string
      cost: string
      currency: string
      note: string
    }) =>
      reconcileSupplierAttempt(
        props.attempt.id,
        values.status,
        values.cost,
        values.currency,
        values.note
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({
        queryKey: ['supplier-routing-attempts'],
      })
      void queryClient.invalidateQueries({
        queryKey: ['supplier-routing-stats'],
      })
      props.onClose()
    },
    onError: (error) => form.setError('root', { message: error.message }),
  })
  return (
    <form
      className='flex flex-wrap items-end gap-3 rounded-md border p-3'
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <Label>
        {t('Verified call result')}
        <select
          className='bg-background ml-2 rounded border p-2'
          {...form.register('status')}
        >
          <option value='success'>{t('Success')}</option>
          <option value='failed'>{t('Failed')}</option>
          <option value='cancelled'>{t('Not sent')}</option>
        </select>
      </Label>
      <Label>
        {t('Procurement cost')}
        <Input
          {...form.register('cost', { required: true })}
          inputMode='decimal'
        />
      </Label>
      <Label>
        {t('Currency')}
        <select
          className='bg-background ml-2 rounded border p-2'
          {...form.register('currency')}
        >
          <option value='CNY'>CNY</option>
          <option value='USD'>USD</option>
        </select>
      </Label>
      <Label>
        {t('Reconciliation evidence')}
        <Input {...form.register('note', { required: true })} />
      </Label>
      <Button type='submit' disabled={save.isPending}>
        {t('Save reconciliation')}
      </Button>
      <Button type='button' variant='outline' onClick={props.onClose}>
        {t('Cancel')}
      </Button>
      {form.formState.errors.root && (
        <p role='alert' className='text-destructive w-full'>
          {form.formState.errors.root.message}
        </p>
      )}
    </form>
  )
}
