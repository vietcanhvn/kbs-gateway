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
import i18next from 'i18next'
import { useState, useCallback } from 'react'
import { toast } from 'sonner'

import { requestPayOSPayment, isApiSuccess } from '../api'

// Chỉ mở link http/https do máy chủ trả về (chặn javascript:, data:...).
function getPaymentUrl(data: unknown): string | null {
  if (!data || typeof data !== 'object' || !('payment_url' in data)) {
    return null
  }
  const raw = data.payment_url
  if (typeof raw !== 'string') return null
  try {
    const url = new URL(raw)
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : null
  } catch {
    return null
  }
}

/**
 * payOS: mở trang mã QR chuyển khoản của payOS. Tiền vào tài khoản thì gateway
 * tự cộng hạn mức (webhook, hoặc khi người dùng quay về từ trang payOS).
 */
export function usePayOSPayment() {
  const [processing, setProcessing] = useState(false)

  const processPayOSPayment = useCallback(async (topupAmount: number) => {
    setProcessing(true)
    try {
      const response = await requestPayOSPayment({
        amount: Math.floor(topupAmount),
      })
      if (isApiSuccess(response)) {
        const paymentUrl = getPaymentUrl(response.data)
        if (paymentUrl) {
          // Cùng tab: mở tab mới sau khi chờ máy chủ bị trình duyệt chặn
          // (popup), và payOS sẽ đưa người dùng quay về trang ví.
          window.location.assign(paymentUrl)
          toast.success(i18next.t('Redirecting to payment page...'))
          return true
        }
      }
      const data = response.data
      toast.error(
        typeof data === 'string' && data.trim()
          ? data
          : response.message || i18next.t('Payment request failed')
      )
      return false
    } catch {
      toast.error(i18next.t('Payment request failed'))
      return false
    } finally {
      setProcessing(false)
    }
  }, [])

  return { processing, processPayOSPayment }
}
