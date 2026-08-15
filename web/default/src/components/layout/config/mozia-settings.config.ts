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
import type { TFunction } from 'i18next'
import { Settings } from 'lucide-react'

import {
  canAccessMoziaSettingsSection,
  getMoziaSettingsSectionNavItems,
} from '@/features/mozia-settings/section-registry'
import { useAuthStore } from '@/stores/auth-store'

import type { NavGroup, SidebarView } from '../types'

function getMoziaSettingsNavGroups(t: TFunction): NavGroup[] {
  const user = useAuthStore.getState().auth.user
  return [
    {
      id: 'mozia-administration',
      title: t('Mozia Administration'),
      items: [
        {
          title: t('Mozia Settings'),
          icon: Settings,
          items: getMoziaSettingsSectionNavItems(t).filter((item) => {
            const section = item.url.split('/').pop()
            return section
              ? canAccessMoziaSettingsSection(section, user)
              : false
          }),
        },
      ],
    },
  ]
}

export const MOZIA_SETTINGS_VIEW: SidebarView = {
  id: 'mozia-settings',
  pathPattern: /^\/mozia-settings(\/|$)/,
  parent: {
    to: '/dashboard/overview',
    label: 'Back to Dashboard',
  },
  getNavGroups: getMoziaSettingsNavGroups,
}
