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

import { buildAboutMarkdown } from '../../about/content'
import { buildGuideMarkdown } from '../content'

const context = { name: 'KBS API', origin: 'https://api.example.com' }

describe('built-in docs page', () => {
  test('Vietnamese UI gets the Vietnamese guide, everything else English', () => {
    assert.match(
      buildGuideMarkdown('vi', context),
      /^# Hướng dẫn sử dụng KBS API/
    )
    assert.match(buildGuideMarkdown('vi-VN', context), /Bắt đầu trong 4 bước/)
    assert.match(buildGuideMarkdown('en', context), /^# KBS API user guide/)
    assert.match(buildGuideMarkdown('zh', context), /Get started in 4 steps/)
    assert.match(
      buildGuideMarkdown(undefined, context),
      /Get started in 4 steps/
    )
  })

  test('tells users where to top up and where to create a key, with the real menu labels', () => {
    const guide = buildGuideMarkdown('vi', context)
    assert.ok(guide.includes('**Bảng điều khiển → Ví**'))
    assert.ok(guide.includes('**Nạp tiền**'))
    assert.ok(guide.includes('**Mô hình** (menu trên cùng)'))
    assert.ok(!guide.includes('Quảng trường'))
    assert.ok(guide.includes('**Bảng điều khiển → Khóa API**'))
    assert.ok(guide.includes('**Tạo Khóa API**'))
    assert.ok(guide.includes('*Lịch sử đơn hàng*'))
  })

  test('explains referral commission: where the link is and that each top-up of an invited user pays', () => {
    const vi = buildGuideMarkdown('vi', context)
    assert.ok(vi.includes('## Giới thiệu bạn bè'))
    assert.ok(vi.includes('**Ví** → *Chương trình Giới thiệu*'))
    assert.ok(vi.includes('**Chuyển vào số dư**'))
    assert.ok(vi.includes('**Lịch sử hoa hồng**'))
    const en = buildGuideMarkdown('en', context)
    assert.ok(en.includes('## Referring friends'))
    assert.ok(en.includes('**Commission history**'))
  })

  test('API examples use the address the page is served from', () => {
    for (const language of ['vi', 'en']) {
      const guide = buildGuideMarkdown(language, context)
      assert.ok(
        guide.includes('curl https://api.example.com/v1/chat/completions')
      )
      assert.ok(guide.includes('base_url="https://api.example.com/v1"'))
      assert.ok(
        guide.includes('https://api.example.com/v1/video/generations/TASK_ID')
      )
      assert.ok(
        guide.includes('https://api.example.com/v1/videos/TASK_ID/content')
      )
      assert.ok(!guide.includes('undefined'))
    }
  })
})

describe('built-in about page', () => {
  test('uses the configured system name and the UI language', () => {
    assert.match(
      buildAboutMarkdown('vi', 'KBS API'),
      /^# KBS API - cổng API tổng hợp/
    )
    assert.match(
      buildAboutMarkdown('en', 'KBS API'),
      /^# KBS API - one API gateway/
    )
  })

  test('keeps the AGPL attribution and the source link', () => {
    for (const language of ['vi', 'en']) {
      const about = buildAboutMarkdown(language, 'KBS API')
      assert.ok(about.includes('https://github.com/QuantumNous/new-api'))
      assert.ok(about.includes('https://github.com/songquanpeng/one-api'))
      assert.ok(about.includes('AGPL v3.0'))
      assert.ok(about.includes('https://github.com/vietcanhvn/kbs-gateway'))
    }
  })
})
