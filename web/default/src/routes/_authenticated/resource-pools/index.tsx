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
import { createFileRoute, redirect } from '@tanstack/react-router'
import { z } from 'zod'

import { SupplierManagementPage } from '@/features/supplier-routing'
import { hasPermission } from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

export const Route = createFileRoute('/_authenticated/resource-pools/')({
  beforeLoad: () => {
    if (!hasPermission(useAuthStore.getState().auth.user, 'channel', 'read')) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: z.object({
    page: z.number().int().positive().optional().catch(1),
    pageSize: z.number().int().positive().max(256).optional().catch(undefined),
    filter: z.string().optional().catch(''),
    supplier: z
      .array(z.string().regex(/^[1-9]\d*$/))
      .optional()
      .catch([]),
    status: z
      .array(z.enum(['enabled', 'disabled']))
      .optional()
      .catch([]),
    model: z.array(z.string()).optional().catch([]),
    failure_domain: z.array(z.string()).optional().catch([]),
  }),
  component: ResourcePoolsPage,
})

function ResourcePoolsPage() {
  const search = Route.useSearch()
  const supplierId =
    search.supplier?.length === 1 ? Number(search.supplier[0]) : undefined
  return <SupplierManagementPage view='pools' supplierId={supplierId} />
}
