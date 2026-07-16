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
import { createElement } from 'react'

import { createSectionRegistry } from '@/features/system-settings/utils/section-registry'

import { MoziaQuotaPolicySection } from './model-quota-policy-section'
import { MoziaUserModelRatioSection } from './user-model-ratio-section'
import { MoziaWalletBalancesSection } from './wallet-balances-section'

type MoziaSettingsState = Record<string, never>

const MOZIA_SETTINGS_SECTIONS = [
  {
    id: 'model-quota-policies',
    titleKey: 'Mozia Model Quota Policies',
    build: () => createElement(MoziaQuotaPolicySection),
  },
  {
    id: 'user-wallet-balances',
    titleKey: 'User Wallet Balances',
    build: () => createElement(MoziaWalletBalancesSection),
  },
  {
    id: 'user-model-ratios',
    titleKey: 'User Billing Ratios',
    build: () => createElement(MoziaUserModelRatioSection),
  },
] as const

export type MoziaSettingsSectionId =
  (typeof MOZIA_SETTINGS_SECTIONS)[number]['id']

const moziaSettingsRegistry = createSectionRegistry<
  MoziaSettingsSectionId,
  MoziaSettingsState
>({
  sections: MOZIA_SETTINGS_SECTIONS,
  defaultSection: 'model-quota-policies',
  basePath: '/mozia-settings',
  urlStyle: 'path',
})

export const MOZIA_SETTINGS_SECTION_IDS = moziaSettingsRegistry.sectionIds
export const MOZIA_SETTINGS_DEFAULT_SECTION =
  moziaSettingsRegistry.defaultSection
export const getMoziaSettingsSectionNavItems =
  moziaSettingsRegistry.getSectionNavItems
export const getMoziaSettingsSectionContent =
  moziaSettingsRegistry.getSectionContent
export const getMoziaSettingsSectionMeta = moziaSettingsRegistry.getSectionMeta
