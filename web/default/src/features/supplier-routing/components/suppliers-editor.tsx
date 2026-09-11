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
import { useFieldArray, useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

import type { SupplierConfigValues } from '../lib/config-schema'
import { ConfigField, ConfigSwitch } from './config-fields'

export function SuppliersEditor() {
  const { t } = useTranslation()
  const form = useFormContext<SupplierConfigValues>()
  const list = useFieldArray({
    control: form.control,
    name: 'suppliers',
    keyName: 'formKey',
  })
  const [suppliers, pools, rules] = useWatch({
    control: form.control,
    name: ['suppliers', 'pools', 'rules'],
  })
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'A supplier is a partner company. One supplier can own several resource pools and channels.'
        )}
      </p>
      {list.fields.map((item, i) => {
        const used =
          pools.some((pool) => pool.supplier_id === item.id) ||
          rules.some((rule) =>
            rule.targets.some((target) => target.supplier_id === item.id)
          )
        return (
          <Card key={item.formKey}>
            <CardHeader className='flex-row items-center justify-between gap-3'>
              <CardTitle>
                {suppliers[i]?.name || t('New supplier')} · #{item.id}
              </CardTitle>
              <Button
                type='button'
                size='sm'
                variant='ghost'
                disabled={used}
                title={
                  used
                    ? t('Remove references before deleting this item.')
                    : undefined
                }
                onClick={() => list.remove(i)}
              >
                {t('Delete')}
              </Button>
            </CardHeader>
            <CardContent className='space-y-4'>
              <div className='grid gap-4 sm:grid-cols-2'>
                <ConfigField
                  name={`suppliers.${i}.name`}
                  label={t('Supplier name')}
                />
                <ConfigField
                  name={`suppliers.${i}.region`}
                  label={t('Region')}
                />
                <ConfigField
                  name={`suppliers.${i}.contact`}
                  label={t('Contact')}
                />
                <ConfigField
                  name={`suppliers.${i}.terms`}
                  label={t('Contract reference')}
                />
              </div>
              <ConfigField
                name={`suppliers.${i}.data_policy`}
                label={t('Data handling policy')}
              />
              <ConfigSwitch
                name={`suppliers.${i}.enabled`}
                label={t('Supplier available')}
              />
              {used && (
                <p className='text-muted-foreground text-xs'>
                  {t('Remove references before deleting this item.')}
                </p>
              )}
            </CardContent>
          </Card>
        )
      })}
      <Button
        type='button'
        variant='outline'
        disabled={list.fields.length >= 128}
        onClick={() =>
          list.append({
            id: Math.max(0, ...suppliers.map((s) => s.id)) + 1,
            name: '',
            enabled: false,
            region: '',
            contact: '',
            data_policy: '',
            terms: '',
          })
        }
      >
        {t('Add supplier')}
      </Button>
    </div>
  )
}
