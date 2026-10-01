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
// ============================================================================
// Referral commission helpers (pure)
// ============================================================================

export interface CommissionPoint {
  created_time: number
  quota: number
}

export interface CommissionDay {
  /** Local calendar day, YYYY-MM-DD. */
  date: string
  /** Short axis label, MM-DD. */
  label: string
  quota: number
  count: number
}

function localDayKey(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${date.getFullYear()}-${month}-${day}`
}

/**
 * Group commissions by the viewer's local calendar day, for the last `days`
 * days ending today. Days without a commission are kept as zero so the chart
 * shows a continuous axis.
 */
export function bucketCommissionsByDay(
  points: CommissionPoint[],
  days: number,
  nowMs: number
): CommissionDay[] {
  const buckets = new Map<string, CommissionDay>()
  const today = new Date(nowMs)
  today.setHours(0, 0, 0, 0)
  for (let offset = days - 1; offset >= 0; offset -= 1) {
    const day = new Date(today)
    day.setDate(today.getDate() - offset)
    const date = localDayKey(day)
    buckets.set(date, { date, label: date.slice(5), quota: 0, count: 0 })
  }
  for (const point of points) {
    const bucket = buckets.get(localDayKey(new Date(point.created_time * 1000)))
    if (bucket) {
      bucket.quota += point.quota
      bucket.count += 1
    }
  }
  return [...buckets.values()]
}

/** 5 -> "5%", 2.5 -> "2.5%". */
export function formatCommissionRate(percent: number): string {
  return `${Number(percent.toFixed(2))}%`
}
