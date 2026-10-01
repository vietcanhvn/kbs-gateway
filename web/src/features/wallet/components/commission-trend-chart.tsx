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
import { VChart } from '@visactor/react-vchart'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { formatQuota, quotaUnitsToDollars } from '@/lib/format'
import { useChartTheme } from '@/lib/use-chart-theme'
import { cn } from '@/lib/utils'
import { VCHART_OPTION } from '@/lib/vchart'

import { bucketCommissionsByDay } from '../lib/commission'
import type { ReferralCommissionPoint } from '../types'

const CHART_DAYS = 30

type DayDatum = { label: string; amount: number; quota: number; count: number }

/** Commission earned per day over the last 30 days. */
export function CommissionTrendChart(props: {
  points: ReferralCommissionPoint[]
  className?: string
}) {
  const { t } = useTranslation()
  const { resolvedTheme, themeReady } = useChartTheme()
  const dark = resolvedTheme === 'dark'
  const textColor = dark
    ? 'rgba(255, 255, 255, 0.68)'
    : 'rgba(15, 23, 42, 0.58)'
  const gridColor = dark
    ? 'rgba(255, 255, 255, 0.12)'
    : 'rgba(15, 23, 42, 0.12)'

  const spec = useMemo(() => {
    const values: DayDatum[] = bucketCommissionsByDay(
      props.points,
      CHART_DAYS,
      Date.now()
    ).map((day) => ({
      label: day.label,
      amount: quotaUnitsToDollars(day.quota),
      quota: day.quota,
      count: day.count,
    }))
    return {
      type: 'bar' as const,
      data: [{ id: 'commission', values }],
      xField: 'label',
      yField: 'amount',
      bar: { style: { cornerRadius: [3, 3, 0, 0] } },
      legends: { visible: false },
      tooltip: {
        mark: {
          title: { value: (datum: DayDatum) => datum.label },
          content: [
            {
              key: t('Commission'),
              value: (datum: DayDatum) => formatQuota(datum.quota),
            },
            {
              key: t('Top-ups'),
              value: (datum: DayDatum) => String(datum.count),
            },
          ],
        },
      },
      axes: [
        {
          orient: 'bottom',
          label: { style: { fill: textColor, fontSize: 10 } },
          tick: { visible: false },
          sampling: true,
        },
        {
          orient: 'left',
          label: { style: { fill: textColor, fontSize: 10 } },
          grid: {
            visible: true,
            style: { lineDash: [3, 3], stroke: gridColor },
          },
        },
      ],
    }
  }, [gridColor, props.points, t, textColor])

  return (
    <div className={cn('h-48 sm:h-56', props.className)}>
      {themeReady && (
        <VChart
          key={`commission-${resolvedTheme}`}
          spec={{
            ...spec,
            theme: dark ? 'dark' : 'light',
            background: 'transparent',
          }}
          option={VCHART_OPTION}
        />
      )}
    </div>
  )
}
