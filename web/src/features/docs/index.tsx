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

import { PublicLayout } from '@/components/layout'
import { RichContent } from '@/components/rich-content'
import { useStatus } from '@/hooks/use-status'
import { DEFAULT_SYSTEM_NAME } from '@/lib/constants'

import { buildGuideMarkdown } from './content'

export function Docs() {
  const { i18n } = useTranslation()
  const { status } = useStatus()
  const name =
    (status?.system_name as string | undefined)?.trim() || DEFAULT_SYSTEM_NAME
  const origin = typeof window === 'undefined' ? '' : window.location.origin

  return (
    <PublicLayout>
      <div className='mx-auto max-w-4xl px-4 py-8'>
        <RichContent
          mode='markdown'
          content={buildGuideMarkdown(i18n.language, { name, origin })}
          className='prose-neutral dark:prose-invert max-w-none'
        />
      </div>
    </PublicLayout>
  )
}
