import { Outlet, useParams } from '@tanstack/react-router'
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
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { SectionPageLayout } from '@/components/layout'
import { SettingsPageProvider } from '@/features/system-settings/components/settings-page-context'

import {
  MOZIA_SETTINGS_DEFAULT_SECTION,
  getMoziaSettingsSectionContent,
  getMoziaSettingsSectionMeta,
  type MoziaSettingsSectionId,
} from './section-registry'

const emptySettings = {}

type MoziaSettingsFrameProps = {
  title: ReactNode
  children: ReactNode
}

function MoziaSettingsFrame(props: MoziaSettingsFrameProps) {
  const [actionsContainer, setActionsContainer] =
    useState<HTMLDivElement | null>(null)
  const [titleStatusContainer, setTitleStatusContainer] =
    useState<HTMLSpanElement | null>(null)

  return (
    <SettingsPageProvider
      actionsContainer={actionsContainer}
      titleStatusContainer={titleStatusContainer}
    >
      <SectionPageLayout>
        <SectionPageLayout.Title>
          <span className='inline-flex max-w-full min-w-0 items-center gap-2 align-middle'>
            <span className='truncate'>{props.title}</span>
            <span
              ref={setTitleStatusContainer}
              className='inline-flex min-w-0 shrink-0 items-center'
            />
          </span>
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <div
            ref={setActionsContainer}
            className='flex flex-wrap items-center justify-end gap-2'
          />
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex w-full flex-col gap-4'>{props.children}</div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </SettingsPageProvider>
  )
}

export function MoziaSettings() {
  return <Outlet />
}

export function MoziaSettingsSectionPage() {
  const { t } = useTranslation()
  const params = useParams({
    from: '/_authenticated/mozia-settings/$section',
  })
  const activeSection = (params?.section ??
    MOZIA_SETTINGS_DEFAULT_SECTION) as MoziaSettingsSectionId
  const sectionMeta = getMoziaSettingsSectionMeta(activeSection)

  return (
    <MoziaSettingsFrame title={t(sectionMeta.titleKey)}>
      {getMoziaSettingsSectionContent(activeSection, emptySettings)}
    </MoziaSettingsFrame>
  )
}
