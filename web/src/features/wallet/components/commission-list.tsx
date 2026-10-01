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
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  StaticDataTable,
  staticDataTableClassNames as tableStyles,
  type StaticDataTableColumn,
} from '@/components/data-table'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota, formatTimestampToDate } from '@/lib/format'

import { getReferralCommissions } from '../api'
import { formatCommissionRate } from '../lib/commission'
import type { ReferralCommissionItem, ReferralCommissionScope } from '../types'

const PAGE_SIZE = 10

/** Paged table of commissions: the inviter's own, or everyone's for admins. */
export function CommissionList(props: {
  scope: ReferralCommissionScope
  emptyText: string
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = useQuery({
    queryKey: ['referral-commissions', props.scope, page],
    queryFn: () => getReferralCommissions(props.scope, page, PAGE_SIZE),
    placeholderData: (previous) => previous,
  })

  if (query.isLoading) {
    return <Skeleton className='h-40 rounded-lg' />
  }

  const items = query.data?.data?.items ?? []
  const total = query.data?.data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  if (items.length === 0) {
    return (
      <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
        {props.emptyText}
      </div>
    )
  }

  const columns: StaticDataTableColumn<ReferralCommissionItem>[] = [
    {
      id: 'time',
      header: t('Time'),
      className: tableStyles.compactHeaderCell,
      cellClassName: tableStyles.compactMutedCell,
      cell: (row) => formatTimestampToDate(row.created_time),
    },
    ...(props.scope === 'all'
      ? [
          {
            id: 'inviter',
            header: t('Referrer'),
            className: tableStyles.compactHeaderCell,
            cellClassName: tableStyles.compactCell,
            cell: (row: ReferralCommissionItem) =>
              row.inviter_name || `#${row.inviter_id ?? ''}`,
          },
        ]
      : []),
    {
      id: 'invitee',
      header: t('Invited user'),
      className: tableStyles.compactHeaderCell,
      cellClassName: tableStyles.compactCell,
      cell: (row) => row.invitee_name || `#${row.invitee_id ?? ''}`,
    },
    {
      id: 'topup',
      header: t('Top-up'),
      className: tableStyles.compactHeaderCellRight,
      cellClassName: tableStyles.compactMutedNumericCell,
      cell: (row) => formatQuota(row.top_up_quota),
    },
    {
      id: 'rate',
      header: t('Rate'),
      className: tableStyles.compactHeaderCellRight,
      cellClassName: tableStyles.compactMutedNumericCell,
      cell: (row) => formatCommissionRate(row.percent),
    },
    {
      id: 'commission',
      header: t('Commission'),
      className: tableStyles.compactHeaderCellRight,
      cellClassName: tableStyles.compactNumericCell,
      cell: (row) => `+${formatQuota(row.quota)}`,
    },
  ]

  return (
    <div className='space-y-3'>
      <StaticDataTable
        className='rounded-lg'
        tableClassName='text-sm'
        headerRowClassName={tableStyles.compactHeaderRow}
        data={items}
        getRowKey={(row) => row.id}
        columns={columns}
      />
      {total > PAGE_SIZE && (
        <div className='flex items-center justify-between gap-3'>
          <div className='text-muted-foreground text-xs'>
            {t('Showing')} {(page - 1) * PAGE_SIZE + 1}-
            {Math.min(page * PAGE_SIZE, total)} {t('of')} {total}
          </div>
          <div className='flex items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setPage(page - 1)}
              disabled={page <= 1}
              className='h-8 w-8 p-0'
              aria-label={t('Previous')}
            >
              <ChevronLeft className='h-4 w-4' />
            </Button>
            <span className='text-muted-foreground text-sm'>
              {page} / {totalPages}
            </span>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => setPage(page + 1)}
              disabled={page >= totalPages}
              className='h-8 w-8 p-0'
              aria-label={t('Next')}
            >
              <ChevronRight className='h-4 w-4' />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
