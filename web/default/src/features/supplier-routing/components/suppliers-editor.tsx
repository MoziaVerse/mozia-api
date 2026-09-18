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
import { useTranslation } from 'react-i18next'

import { ConfigField, ConfigSwitch } from './config-fields'

export function SupplierFields({ index }: { index: number }) {
  const { t } = useTranslation()
  return (
    <div className='space-y-5'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Start with a supplier name. Add capacity, models and channel bindings when ready to connect.'
        )}
      </p>
      <ConfigField
        name={`suppliers.${index}.name`}
        label={t('Supplier name')}
      />
      <div className='grid gap-4 sm:grid-cols-2'>
        <ConfigField name={`suppliers.${index}.region`} label={t('Region')} />
        <ConfigField name={`suppliers.${index}.contact`} label={t('Contact')} />
      </div>
      <ConfigSwitch
        name={`suppliers.${index}.enabled`}
        label={t('Supplier available')}
      />
      <details>
        <summary className='cursor-pointer text-sm'>
          {t('Contract and data policy')}
        </summary>
        <div className='mt-4 space-y-4'>
          <ConfigField
            name={`suppliers.${index}.terms`}
            label={t('Contract reference')}
          />
          <ConfigField
            name={`suppliers.${index}.data_policy`}
            label={t('Data handling policy')}
          />
        </div>
      </details>
    </div>
  )
}
