import { describe, test } from 'bun:test'
import assert from 'node:assert/strict'

import { convertDetectedLanguage } from './languages'

describe('convertDetectedLanguage (KBS 12/11)', () => {
  test('keeps our own Chinese codes from the localStorage cache', () => {
    assert.equal(convertDetectedLanguage('zhTW'), 'zhTW')
    assert.equal(convertDetectedLanguage('zhCN'), 'zhCN')
  })

  test('maps browser tags', () => {
    assert.equal(convertDetectedLanguage('zh-TW'), 'zhTW')
    assert.equal(convertDetectedLanguage('zh-Hant-HK'), 'zhTW')
    assert.equal(convertDetectedLanguage('zh'), 'zhCN')
    assert.equal(convertDetectedLanguage('vi'), 'vi')
  })
})
