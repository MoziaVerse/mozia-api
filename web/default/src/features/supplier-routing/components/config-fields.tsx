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
import { useId } from 'react'
import { Controller, useFormContext, type FieldPath } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  Field,
  FieldContent,
  FieldDescription,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'

import type { SupplierConfigValues } from '../lib/config-schema'

export function ConfigField(props: {
  name: FieldPath<SupplierConfigValues>
  label: string
  description?: string
  type?: 'number' | 'text'
  min?: number
  max?: number
  readOnly?: boolean
  onValueChange?: () => void
  options?: { value: string | number; label: string }[]
}) {
  const { t } = useTranslation()
  const id = useId()
  const form = useFormContext<SupplierConfigValues>()
  const error = form.getFieldState(props.name, form.formState).error
  const registration = form.register(props.name, {
    valueAsNumber: props.type === 'number',
  })
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{props.label}</FieldLabel>
      {props.options ? (
        <NativeSelect
          id={id}
          className='w-full'
          aria-invalid={Boolean(error)}
          aria-describedby={`${id}-description`}
          {...registration}
          onChange={(event) => {
            void registration.onChange(event)
            props.onValueChange?.()
          }}
        >
          <NativeSelectOption value=''>
            {t('Select an option')}
          </NativeSelectOption>
          {props.options.map((option) => (
            <NativeSelectOption key={option.value} value={option.value}>
              {option.label}
            </NativeSelectOption>
          ))}
        </NativeSelect>
      ) : (
        <Input
          id={id}
          type={props.type ?? 'text'}
          min={props.min}
          max={props.max}
          step={props.type === 'number' ? 1 : undefined}
          readOnly={props.readOnly}
          aria-invalid={Boolean(error)}
          aria-describedby={`${id}-description`}
          {...registration}
        />
      )}
      <FieldDescription id={`${id}-description`}>
        {error
          ? t('Check this value and its allowed range.')
          : props.description}
      </FieldDescription>
    </Field>
  )
}

export function ConfigSwitch(props: {
  name: FieldPath<SupplierConfigValues>
  label: string
  description?: string
  disabled?: boolean
}) {
  const id = useId()
  const form = useFormContext<SupplierConfigValues>()
  return (
    <Field orientation='horizontal' data-disabled={props.disabled}>
      <FieldContent>
        <FieldLabel htmlFor={id}>{props.label}</FieldLabel>
        <FieldDescription id={`${id}-description`}>
          {props.description}
        </FieldDescription>
      </FieldContent>
      <Controller
        control={form.control}
        name={props.name}
        render={({ field }) => (
          <Switch
            id={id}
            name={field.name}
            checked={Boolean(field.value)}
            onCheckedChange={field.onChange}
            onBlur={field.onBlur}
            ref={field.ref}
            disabled={props.disabled}
            aria-describedby={`${id}-description`}
          />
        )}
      />
    </Field>
  )
}
