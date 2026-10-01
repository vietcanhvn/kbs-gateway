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
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  staticDataTableClassNames as tableStyles,
} from '@/components/data-table'
import { Skeleton } from '@/components/ui/skeleton'
import { getAdminReferralStats } from '@/features/wallet/api'
import { CommissionList } from '@/features/wallet/components/commission-list'
import { CommissionTiles } from '@/features/wallet/components/commission-tiles'
import { CommissionTrendChart } from '@/features/wallet/components/commission-trend-chart'
import { formatQuota } from '@/lib/format'

/** System-wide referral commission statistics for administrators. */
export function ReferralCommissionStats() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['referral-admin-stats'],
    queryFn: getAdminReferralStats,
  })

  if (query.isLoading) {
    return <Skeleton className='h-64 rounded-lg' />
  }

  const stats = query.data?.data
  const summary = stats?.summary
  const top = stats?.top ?? []

  return (
    <div className='space-y-5'>
      <CommissionTiles
        items={[
          {
            label: t('Total commission paid'),
            value: formatQuota(summary?.total_quota ?? 0),
          },
          {
            label: t('Top-ups from invited users'),
            value: formatQuota(summary?.total_top_up_quota ?? 0),
          },
          {
            label: t('Referrers earning'),
            value: String(summary?.inviter_count ?? 0),
          },
          {
            label: t('Accounts from referral links'),
            value: String(stats?.referred_users ?? 0),
          },
        ]}
      />

      <div>
        <h4 className='mb-1 text-sm font-medium'>
          {t('Commission per day, last 30 days')}
        </h4>
        <CommissionTrendChart points={stats?.recent ?? []} />
      </div>

      <div>
        <h4 className='mb-2 text-sm font-medium'>{t('Top referrers')}</h4>
        {top.length === 0 ? (
          <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
            {t('No commission has been paid yet.')}
          </div>
        ) : (
          <StaticDataTable
            className='rounded-lg'
            tableClassName='text-sm'
            headerRowClassName={tableStyles.compactHeaderRow}
            data={top}
            getRowKey={(row) => row.inviter_id}
            columns={[
              {
                id: 'rank',
                header: '#',
                className: `${tableStyles.compactHeaderCell} w-12`,
                cellClassName: tableStyles.compactMutedCell,
                cell: (_row, index) => index + 1,
              },
              {
                id: 'referrer',
                header: t('Referrer'),
                className: tableStyles.compactHeaderCell,
                cellClassName: tableStyles.compactCell,
                cell: (row) => row.username || `#${row.inviter_id}`,
              },
              {
                id: 'invitees',
                header: t('Invited users who topped up'),
                className: tableStyles.compactHeaderCellRight,
                cellClassName: tableStyles.compactMutedNumericCell,
                cell: (row) => row.invitee_count,
              },
              {
                id: 'payments',
                header: t('Commission payments'),
                className: tableStyles.compactHeaderCellRight,
                cellClassName: tableStyles.compactMutedNumericCell,
                cell: (row) => row.total_count,
              },
              {
                id: 'commission',
                header: t('Commission'),
                className: tableStyles.compactHeaderCellRight,
                cellClassName: tableStyles.compactNumericCell,
                cell: (row) => formatQuota(row.total_quota),
              },
            ]}
          />
        )}
      </div>

      <div>
        <h4 className='mb-2 text-sm font-medium'>{t('All commissions')}</h4>
        <CommissionList
          scope='all'
          emptyText={t('No commission has been paid yet.')}
        />
      </div>
    </div>
  )
}
