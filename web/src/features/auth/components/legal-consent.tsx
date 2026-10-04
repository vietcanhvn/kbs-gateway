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
import { AlertCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

import type { SystemStatus } from '../types'

interface LegalConsentProps {
  status: SystemStatus | null
  checked: boolean
  onCheckedChange: (nextValue: boolean) => void
  className?: string
  /**
   * Attention state: destructive border, a short shake and a reminder line
   * underneath. Set it when the visitor tried to sign in or sign up without
   * ticking the box.
   */
  highlight?: boolean
}

export function LegalConsent({
  status,
  checked,
  onCheckedChange,
  className,
  highlight = false,
}: LegalConsentProps) {
  const { t } = useTranslation()
  const hasUserAgreement = Boolean(status?.user_agreement_enabled)
  const hasPrivacyPolicy = Boolean(status?.privacy_policy_enabled)

  if (!hasUserAgreement && !hasPrivacyPolicy) {
    return null
  }

  const handleChange = (value: boolean) => {
    onCheckedChange(value === true)
  }

  const linkClassName =
    'text-primary font-medium underline underline-offset-2 hover:no-underline'

  return (
    <div className={cn('space-y-1.5', className)}>
      <div
        id='legal-consent-box'
        className={cn(
          'flex items-start gap-3 rounded-md border p-3 transition-colors',
          highlight
            ? 'border-destructive bg-destructive/5 legal-consent-shake'
            : 'border-input bg-muted/50'
        )}
      >
        <Checkbox
          id='legal-consent'
          checked={checked}
          onCheckedChange={handleChange}
          aria-invalid={highlight}
          aria-describedby={highlight ? 'legal-consent-error' : undefined}
          /*
           * This one checkbox carries a legal decision, so it is drawn louder
           * than the shared default:
           *  - border-2 + border-foreground, because --input is oklch(0.93) on
           *    white and white/17% on dark, which both read as "barely there".
           *  - rounded-[4px], because the theme's --radius is 1rem, so the
           *    default rounded-sm lands near 10px and a 20px box turns into a
           *    circle that nobody recognises as a checkbox.
           *  - size-5 keeps it at 20px, a comfortable tap target.
           */
          className='border-foreground mt-0.5 size-5 rounded-[4px] border-2'
        />
        <Label
          htmlFor='legal-consent'
          className='text-foreground items-start gap-1 text-left text-sm leading-6 font-normal'
        >
          <span>
            {t('I have read and agree to the')}{' '}
            {hasUserAgreement && (
              <a
                href='/user-agreement'
                target='_blank'
                rel='noopener noreferrer'
                className={linkClassName}
              >
                {t('User Agreement')}
              </a>
            )}
            {hasUserAgreement && hasPrivacyPolicy && ` ${t('and the')} `}
            {hasPrivacyPolicy && (
              <a
                href='/privacy-policy'
                target='_blank'
                rel='noopener noreferrer'
                className={linkClassName}
              >
                {t('Privacy Policy')}
              </a>
            )}
            .
          </span>
        </Label>
      </div>

      {highlight && (
        <p
          id='legal-consent-error'
          role='alert'
          aria-live='polite'
          className='text-destructive flex items-start gap-1.5 text-sm font-medium'
        >
          <AlertCircle className='mt-0.5 h-4 w-4 shrink-0' />
          <span>
            {t(
              'Please check the box to agree to the User Agreement and Privacy Policy'
            )}
          </span>
        </p>
      )}
    </div>
  )
}
