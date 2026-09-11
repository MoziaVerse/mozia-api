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
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { getSupplierRouting } from '@/features/supplier-routing/api'
import { SupplierConfigEditor } from '@/features/supplier-routing/components/config-editor'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { SettingsSection } from '../components/settings-section'

export function SupplierRoutingSection() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canRead = hasPermission(user, 'channel', 'read')
  const config = useQuery({
    queryKey: ['supplier-routing'],
    queryFn: getSupplierRouting,
    enabled: canRead,
    refetchOnWindowFocus: false,
  })
  if (!canRead) return null
  return (
    <SettingsSection title={t('Supplier routing')}>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Configure suppliers, shared capacity and routing rules here. View traffic and call records in Supplier monitoring.'
          )}
        </p>
        <Button variant='outline' render={<Link to='/supplier-monitor' />}>
          {t('Open supplier monitoring')}
        </Button>
      </div>
      {config.isPending && <p>{t('Loading...')}</p>}
      {config.error && <p role='alert'>{config.error.message}</p>}
      {config.data && (
        <SupplierConfigEditor
          key={config.data.config.revision}
          data={config.data}
          canPublish={hasPermission(user, 'supplier_routing', 'publish')}
          canPreview={hasPermission(user, 'channel', 'operate')}
        />
      )}
    </SettingsSection>
  )
}
