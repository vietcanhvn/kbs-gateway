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
import { useLocation, useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { formatQuota } from '@/lib/format'

import { getReferralStats } from '../api'
import { REFERRAL_STATS_QUERY_KEY } from '../constants'

const SESSION_KEY = 'referral-commission-toast'

/**
 * App-wide congratulation: when the signed-in user has referral commission
 * they have not looked at yet, show one toast per browser session pointing to
 * the wallet. The wallet page itself shows a banner instead.
 */
export function ReferralCommissionNotifier() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const pathname = useLocation({ select: (location) => location.pathname })
  const query = useQuery({
    queryKey: REFERRAL_STATS_QUERY_KEY,
    queryFn: getReferralStats,
    staleTime: 60_000,
  })
  const unseenCount = query.data?.data?.summary?.unseen_count ?? 0
  const unseenQuota = query.data?.data?.summary?.unseen_quota ?? 0

  useEffect(() => {
    if (unseenCount === 0 || pathname.startsWith('/wallet')) return
    const marker = `${unseenCount}:${unseenQuota}`
    if (window.sessionStorage.getItem(SESSION_KEY) === marker) return
    window.sessionStorage.setItem(SESSION_KEY, marker)
    toast.success(
      t(
        'Congratulations! You earned {{amount}} in referral commission from {{count}} new top-up(s).',
        { amount: formatQuota(unseenQuota), count: unseenCount }
      ),
      {
        duration: 12_000,
        action: {
          label: t('View'),
          onClick: () => navigate({ to: '/wallet' }),
        },
      }
    )
  }, [navigate, pathname, t, unseenCount, unseenQuota])

  return null
}
