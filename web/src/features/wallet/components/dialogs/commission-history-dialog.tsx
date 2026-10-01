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

import { Dialog } from '@/components/dialog'
import { formatQuota } from '@/lib/format'

import { formatCommissionRate } from '../../lib/commission'
import type { ReferralStats } from '../../types'
import { CommissionList } from '../commission-list'
import { CommissionTiles } from '../commission-tiles'
import { CommissionTrendChart } from '../commission-trend-chart'

/** The inviter's commission statistics: totals, per-day chart and every payment. */
export function CommissionHistoryDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  stats: ReferralStats | null
  inviteCount: number
}) {
  const { t } = useTranslation()
  const summary = props.stats?.summary
  const percent = props.stats?.percent ?? 0

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Referral commission')}
      description={
        percent > 0
          ? t('You earn {{rate}} of every top-up made by people you invited.', {
              rate: formatCommissionRate(percent),
            })
          : t('Commission earned from top-ups made by people you invited.')
      }
      contentClassName='flex max-h-[calc(100dvh-2rem)] flex-col max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-3xl'
      contentHeight='auto'
      bodyClassName='space-y-4'
    >
      <CommissionTiles
        items={[
          {
            label: t('Total commission'),
            value: formatQuota(summary?.total_quota ?? 0),
          },
          {
            label: t('Commission payments'),
            value: String(summary?.total_count ?? 0),
          },
          { label: t('Invites'), value: String(props.inviteCount) },
          {
            label: t('Invited users who topped up'),
            value: String(summary?.paying_invitees ?? 0),
          },
        ]}
      />
      <div>
        <div className='text-muted-foreground mb-1 text-xs font-medium'>
          {t('Commission per day, last 30 days')}
        </div>
        <CommissionTrendChart points={props.stats?.recent ?? []} />
      </div>
      {props.open && (
        <CommissionList
          scope='self'
          emptyText={t(
            'No commission yet. Share your referral link: you earn each time someone you invited tops up.'
          )}
        />
      )}
    </Dialog>
  )
}
