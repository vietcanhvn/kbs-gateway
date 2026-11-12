import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'
import i18next from 'i18next'

import { formatLanguageMoney, languageMoney } from './view-currency'

describe('money in the interface language (KBS 12/11)', () => {
  test('dong to the whole dong, New Taiwan dollars with NT$', () => {
    assert.equal(formatLanguageMoney(256559.12, 'VND'), '256.559 ₫')
    assert.equal(formatLanguageMoney(236.4, 'VND'), '236 ₫')
    assert.equal(formatLanguageMoney(328.8, 'TWD'), 'NT$329')
    assert.equal(formatLanguageMoney(9.86, 'TWD'), 'NT$9.9')
    assert.equal(formatLanguageMoney(256559, 'VND', { showSymbol: false }), '256.559')
  })

  test('only Vietnamese and Traditional Chinese switch currency', async () => {
    await i18next.init({ lng: 'vi', resources: {} })
    assert.equal(languageMoney()?.currency, 'VND')
    await i18next.changeLanguage('zhTW')
    assert.equal(languageMoney()?.currency, 'TWD')
    await i18next.changeLanguage('en')
    assert.equal(languageMoney(), null)
  })
})
