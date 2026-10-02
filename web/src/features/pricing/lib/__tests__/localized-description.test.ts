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

import { localizeModelDescription } from '../localized-description'

const bilingual =
  '[vi] Tính tiền theo giây: khoảng 11.400đ/giây.\n[en] Billed per second: $0.44/s.'

describe('localizeModelDescription', () => {
  test('shows only the block of the interface language', () => {
    assert.equal(
      localizeModelDescription(bilingual, 'vi'),
      'Tính tiền theo giây: khoảng 11.400đ/giây.'
    )
    assert.equal(
      localizeModelDescription(bilingual, 'en'),
      'Billed per second: $0.44/s.'
    )
  })

  test('matches a regional language code and falls back to English, then to the first block', () => {
    assert.equal(
      localizeModelDescription(bilingual, 'vi-VN'),
      'Tính tiền theo giây: khoảng 11.400đ/giây.'
    )
    assert.equal(
      localizeModelDescription(bilingual, 'ja'),
      'Billed per second: $0.44/s.'
    )
    assert.equal(
      localizeModelDescription('[vi] Chỉ có tiếng Việt.', 'fr'),
      'Chỉ có tiếng Việt.'
    )
  })

  test('keeps multi-line blocks and leaves text without markers untouched', () => {
    assert.equal(
      localizeModelDescription('[vi] Dòng 1\nDòng 2\n[en] Line 1', 'vi'),
      'Dòng 1\nDòng 2'
    )
    assert.equal(
      localizeModelDescription('Plain text with [brackets] inside.', 'vi'),
      'Plain text with [brackets] inside.'
    )
    assert.equal(localizeModelDescription(undefined, 'vi'), '')
  })
})
