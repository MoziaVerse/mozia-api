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
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { FieldSet, FieldLegend } from '@/components/ui/field'

import {
  supplierRoutingMode,
  type SupplierConfigValues,
} from '../lib/config-schema'
import { ConfigField, ConfigSwitch } from './config-fields'

export function RoutingControls() {
  const { t } = useTranslation()
  const { control } = useFormContext<SupplierConfigValues>()
  const [enabled, shadow, canary] = useWatch({
    control,
    name: ['enabled', 'shadow', 'canary_percent'],
  })
  const mode = supplierRoutingMode({ enabled, shadow, canary_percent: canary })
  const modes = {
    legacy: t('Original channel routing'),
    observe: t('Observe without switching traffic'),
    canary: t('Gradual customer rollout'),
    active: t('Dynamic routing for all matched customers'),
  }
  return (
    <FieldSet>
      <FieldLegend>{t('How traffic is routed')}</FieldLegend>
      <Alert>
        <AlertTitle>
          {t('After publishing')}: {modes[mode]}
        </AlertTitle>
        <AlertDescription>
          {t(
            'Only requests matching a routing rule are eligible. Shared pool capacity limits remain active in every mode.'
          )}
        </AlertDescription>
      </Alert>
      <ConfigSwitch
        name='enabled'
        label={t('Enable dynamic supplier routing')}
        description={t(
          'Allow matching requests to choose suppliers using your routing rules. When off, requests keep the original channel routing.'
        )}
      />
      <ConfigSwitch
        name='shadow'
        disabled={!enabled}
        label={t('Observe routing recommendations only')}
        description={t(
          'Record which supplier the new strategy recommends while sending requests through the original route. Recommendations do not call the model or reserve capacity.'
        )}
      />
      <div className='max-w-xl'>
        <ConfigField
          name='canary_percent'
          type='number'
          min={0}
          max={100}
          label={t('Customer rollout percentage')}
          description={t(
            'Users are placed in stable groups by user ID. At 10%, approximately 10% of eligible users use dynamic routing; the others remain in observation mode. This is not a request percentage.'
          )}
        />
      </div>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Rollout takes effect only when dynamic routing is enabled and observation is off. 0% observes everyone; 100% routes all matching users.'
        )}
      </p>
      <details className='rounded-lg border p-4'>
        <summary className='cursor-pointer font-medium'>
          {t('Getting started with supplier routing')}
        </summary>
        <ol className='text-muted-foreground mt-3 list-decimal space-y-2 pl-5 text-sm'>
          <li>
            {t(
              'Add suppliers and verified resource pools, then bind existing channels and models.'
            )}
          </li>
          <li>
            {t(
              'Create routing rules. Publish in observation mode and inspect recommendations in Supplier monitoring.'
            )}
          </li>
          <li>
            {t(
              'Turn off observation and start with a small customer rollout. Increase it after checking traffic, errors and latency.'
            )}
          </li>
          <li>
            {t(
              'To return to original routing, disable dynamic routing and publish. Shared capacity protection stays active.'
            )}
          </li>
        </ol>
      </details>
    </FieldSet>
  )
}
