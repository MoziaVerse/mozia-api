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
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { FieldGroup } from '@/components/ui/field'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
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
  createUserModelRatioSchema,
  type UserModelRatioFormValues,
} from '../lib/user-model-ratio-schema'
import type { MoziaUserRatioScope } from '../types'

type UserModelRatioDialogProps = {
  open: boolean
  editing: boolean
  pending: boolean
  defaultValues: UserModelRatioFormValues
  onOpenChange: (open: boolean) => void
  onSubmit: (values: UserModelRatioFormValues) => Promise<void>
}

export function UserModelRatioDialog(props: UserModelRatioDialogProps) {
  const { t } = useTranslation()
  const schema = useMemo(() => createUserModelRatioSchema(t), [t])
  const form = useForm<UserModelRatioFormValues>({
    resolver: zodResolver(schema),
    defaultValues: props.defaultValues,
  })
  const scope = form.watch('scope')

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {props.editing
              ? t('Edit user billing ratio')
              : t('Add user billing ratio')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Set the billing multiplier for one user, scoped to a model or an entire channel.'
            )}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <form onSubmit={form.handleSubmit(props.onSubmit)}>
            <FieldGroup>
              <div className='grid gap-4 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='user_id'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('User ID')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          min={1}
                          step={1}
                          value={field.value === 0 ? '' : field.value}
                          disabled={props.editing || props.pending}
                          onChange={(event) =>
                            field.onChange(event.target.valueAsNumber)
                          }
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='scope'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Scope')}</FormLabel>
                      <FormControl>
                        <Select<MoziaUserRatioScope>
                          items={[
                            { value: 'model', label: t('Model') },
                            { value: 'channel', label: t('Channel') },
                          ]}
                          value={field.value}
                          disabled={props.editing || props.pending}
                          onValueChange={(value) => {
                            if (value === null) {
                              return
                            }
                            field.onChange(value)
                            if (value === 'model') {
                              form.setValue('channel_id', undefined)
                            } else {
                              form.setValue('model', '')
                            }
                          }}
                        >
                          <SelectTrigger className='w-full'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            <SelectGroup>
                              <SelectItem value='model'>
                                {t('Model')}
                              </SelectItem>
                              <SelectItem value='channel'>
                                {t('Channel')}
                              </SelectItem>
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              {scope === 'model' ? (
                <FormField
                  control={form.control}
                  name='model'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Model')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          value={field.value ?? ''}
                          placeholder='video-v1'
                          disabled={props.editing || props.pending}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'This rule only applies when the requested model matches exactly.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              ) : (
                <FormField
                  control={form.control}
                  name='channel_id'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Channel ID')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          min={1}
                          step={1}
                          value={field.value ?? ''}
                          placeholder='35'
                          disabled={props.editing || props.pending}
                          onChange={(event) => {
                            field.onChange(
                              event.target.value === ''
                                ? undefined
                                : event.target.valueAsNumber
                            )
                          }}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'This rule applies to every model billed through the channel.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}

              <FormField
                control={form.control}
                name='ratio'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Billing multiplier')}</FormLabel>
                    <FormControl>
                      <Input
                        {...field}
                        type='number'
                        min={0}
                        step='any'
                        placeholder='0.36'
                        disabled={props.pending}
                        onChange={(event) =>
                          field.onChange(event.target.valueAsNumber)
                        }
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Use 1 to keep the calculated amount unchanged, or 0.36 to charge 36% of it.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              {props.editing ? (
                <p className='text-muted-foreground text-sm'>
                  {t(
                    'User ID, scope, and target are locked while editing. Delete this rule and add a new one to change them.'
                  )}
                </p>
              ) : null}

              <DialogFooter>
                <Button
                  type='button'
                  variant='outline'
                  disabled={props.pending}
                  onClick={() => props.onOpenChange(false)}
                >
                  {t('Cancel')}
                </Button>
                <Button type='submit' disabled={props.pending}>
                  {props.pending ? <Spinner data-icon='inline-start' /> : null}
                  {props.editing ? t('Save changes') : t('Add rule')}
                </Button>
              </DialogFooter>
            </FieldGroup>
          </form>
        </Form>
      </DialogContent>
    </Dialog>
  )
}
