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
import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import { bucketCommissionsByDay, formatCommissionRate } from '../commission'

const at = (year: number, month: number, day: number, hour = 12) =>
  Math.floor(new Date(year, month - 1, day, hour).getTime() / 1000)

describe('bucketCommissionsByDay', () => {
  const now = new Date(2026, 9, 3, 15).getTime()

  test('returns one zero-filled bucket per day ending today when there are no commissions', () => {
    const days = bucketCommissionsByDay([], 3, now)
    assert.deepEqual(
      days.map((day) => [day.date, day.label, day.quota, day.count]),
      [
        ['2026-10-01', '10-01', 0, 0],
        ['2026-10-02', '10-02', 0, 0],
        ['2026-10-03', '10-03', 0, 0],
      ]
    )
  })

  test('sums commissions of the same local day and ignores ones outside the window', () => {
    const days = bucketCommissionsByDay(
      [
        { created_time: at(2026, 10, 3, 1), quota: 100 },
        { created_time: at(2026, 10, 3, 23), quota: 50 },
        { created_time: at(2026, 10, 1), quota: 7 },
        { created_time: at(2026, 9, 20), quota: 999 },
      ],
      3,
      now
    )
    assert.deepEqual(
      days.map((day) => [day.label, day.quota, day.count]),
      [
        ['10-01', 7, 1],
        ['10-02', 0, 0],
        ['10-03', 150, 2],
      ]
    )
  })
})

describe('formatCommissionRate', () => {
  test('drops trailing zeros and keeps up to two decimals', () => {
    assert.equal(formatCommissionRate(5), '5%')
    assert.equal(formatCommissionRate(2.5), '2.5%')
    assert.equal(formatCommissionRate(3.333), '3.33%')
    assert.equal(formatCommissionRate(0), '0%')
  })
})
