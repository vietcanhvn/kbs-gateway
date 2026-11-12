/*
 * KBS (12/11): show money in the currency of the interface language - Vietnamese
 * → dong (₫), Traditional Chinese → New Taiwan dollars (NT$), others → the
 * admin-configured display (US dollars). Only for DISPLAY of USD amounts
 * (balance, usage logs, statistics, model prices): payment amounts and the
 * admin input forms keep the configured currency (lib/currency.ts billing
 * helpers and getCurrencyDisplay()).
 *
 * Rates come from the KBS app (https://app.kimbox.studio/api/storage/fx-rates),
 * cached in the browser for 30 min; fallback rates until they arrive. Dong is
 * the payOS top-up rate (owner 12/11: top up 30.000 ₫ and the wallet shows
 * 30.000 ₫ - the same rate both ways, so the books never drift), shown to the
 * whole dong; NT$ follows the market rate (open.er-api.com).
 */
import i18next from 'i18next'

export type LanguageCurrency = 'VND' | 'TWD'

const FX_URL = 'https://app.kimbox.studio/api/storage/fx-rates'
const STORAGE_KEY = 'kbs.fxRates.v2'
const MAX_AGE_MS = 30 * 60 * 1000

let rates: Record<LanguageCurrency, number> = { VND: 25000, TWD: 32 }

function acceptRates(next: Record<string, unknown> | null | undefined) {
  const vnd = Number(next?.VND)
  const twd = Number(next?.TWD)
  rates = {
    VND: vnd > 1000 && vnd < 1_000_000 ? vnd : rates.VND,
    TWD: twd > 1 && twd < 1000 ? twd : rates.TWD,
  }
}

/** Currency and rate (1 USD = rate) for the current interface language, or null (keep the configured display). */
export function languageMoney(): { currency: LanguageCurrency; rate: number } | null {
  const lng = String(i18next.resolvedLanguage || i18next.language || '')
  if (lng === 'vi' || lng.startsWith('vi-')) return { currency: 'VND', rate: rates.VND }
  if (lng === 'zhTW' || lng === 'zh-TW' || lng === 'zh-Hant') return { currency: 'TWD', rate: rates.TWD }
  return null
}

/** "256.637 ₫", "NT$329" - dong to the whole dong (balances must match what was paid). */
export function formatLanguageMoney(
  amount: number,
  currency: LanguageCurrency,
  options: { showSymbol?: boolean; compact?: boolean } = {}
): string {
  const abs = Math.abs(amount)
  if (currency === 'VND') {
    const value = abs >= 1 ? Math.round(amount) : amount
    const text = new Intl.NumberFormat('vi-VN', {
      notation: options.compact ? 'compact' : 'standard',
      maximumFractionDigits: abs >= 1 ? 0 : 2,
    }).format(value)
    return options.showSymbol === false ? text : `${text} ₫`
  }
  let digits = 2
  if (abs >= 100) digits = 0
  else if (abs >= 1) digits = 1
  const text = new Intl.NumberFormat('zh-TW', {
    notation: options.compact ? 'compact' : 'standard',
    maximumFractionDigits: options.compact ? 1 : digits,
  }).format(amount)
  return options.showSymbol === false ? text : `NT$${text}`
}

/** Read the rates (browser cache under 6 h, else the KBS app). Errors keep the current rates. */
export async function loadFxRates(now = Date.now()): Promise<void> {
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null')
    if (saved?.rates && now - Number(saved.at) < MAX_AGE_MS) {
      acceptRates(saved.rates)
      return
    }
  } catch {
    // storage blocked: ask the app
  }
  try {
    const response = await fetch(FX_URL, { headers: { Accept: 'application/json' } })
    const body = await response.json()
    if (!response.ok || !body?.rates) return
    acceptRates(body.rates)
    if (body.source !== 'fallback') {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({ rates: body.rates, at: now }))
    }
  } catch {
    // keep the current rates
  }
}
