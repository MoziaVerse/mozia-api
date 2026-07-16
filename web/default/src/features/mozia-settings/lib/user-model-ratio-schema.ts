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

import { z } from 'zod'

type Translate = (key: string) => string

export function createUserModelRatioSchema(t: Translate) {
  return z.object({
    user_id: z
      .number(t('Enter a valid user ID'))
      .int(t('User ID must be an integer'))
      .positive(t('User ID must be greater than zero')),
    model: z.string().trim().min(1, t('Model is required')),
    ratio: z
      .number(t('Enter a valid ratio'))
      .finite(t('Enter a valid ratio'))
      .positive(t('Ratio must be greater than zero')),
  })
}

export type UserModelRatioFormValues = z.infer<
  ReturnType<typeof createUserModelRatioSchema>
>
