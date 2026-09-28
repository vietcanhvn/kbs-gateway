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
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

// payOS (payos.vn): nạp tiền bằng chuyển khoản ngân hàng qua mã VietQR.
// API Key và Checksum Key là bí mật: máy chủ không trả lại, nên ô để trống =
// giữ nguyên key đã lưu.

const schema = z.object({
  PayOSEnabled: z.boolean(),
  PayOSClientId: z.string(),
  PayOSApiKey: z.string(),
  PayOSChecksumKey: z.string(),
  PayOSUnitPrice: z.coerce.number().min(0),
  PayOSMinTopUp: z.coerce.number().int().min(1),
})

type Values = z.infer<typeof schema>

export function PayOSSettingsSection({
  defaultValues,
  callbackAddress,
  complianceConfirmed,
}: {
  defaultValues: {
    PayOSEnabled: boolean
    PayOSClientId: string
    PayOSUnitPrice: number
    PayOSMinTopUp: number
  }
  callbackAddress?: string
  complianceConfirmed: boolean
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const initial: Values = {
    ...defaultValues,
    PayOSApiKey: '',
    PayOSChecksumKey: '',
  }
  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: initial,
  })
  const { isDirty, isSubmitting } = form.formState
  const busy = updateOption.isPending || isSubmitting

  const base = (callbackAddress || window.location.origin).replace(/\/+$/, '')
  const webhookUrl = `${base}/api/payos/webhook`

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []
    const changed = <K extends keyof Values>(key: K) =>
      values[key] !== form.formState.defaultValues?.[key]

    if (changed('PayOSClientId')) {
      updates.push({ key: 'PayOSClientId', value: values.PayOSClientId.trim() })
    }
    if (values.PayOSApiKey.trim()) {
      updates.push({ key: 'PayOSApiKey', value: values.PayOSApiKey.trim() })
    }
    if (values.PayOSChecksumKey.trim()) {
      updates.push({
        key: 'PayOSChecksumKey',
        value: values.PayOSChecksumKey.trim(),
      })
    }
    if (changed('PayOSUnitPrice')) {
      updates.push({
        key: 'PayOSUnitPrice',
        value: String(values.PayOSUnitPrice),
      })
    }
    if (changed('PayOSMinTopUp')) {
      updates.push({ key: 'PayOSMinTopUp', value: String(values.PayOSMinTopUp) })
    }
    // Bật sau cùng, khi key đã lưu xong.
    if (changed('PayOSEnabled')) {
      updates.push({ key: 'PayOSEnabled', value: String(values.PayOSEnabled) })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
    form.reset({ ...values, PayOSApiKey: '', PayOSChecksumKey: '' })
  }

  return (
    <SettingsSection title={t('payOS (VietQR)')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={busy}
            isSaveDisabled={!isDirty}
            saveLabel='Save payOS settings'
          />

          {!complianceConfirmed && (
            <Alert>
              <AlertDescription>
                {t(
                  'Online payments stay hidden until the payment compliance terms are confirmed in Payment Gateway.'
                )}
              </AlertDescription>
            </Alert>
          )}

          <FormField
            control={form.control}
            name='PayOSEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable payOS bank transfer top-up')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Users scan a VietQR code with their banking app; the balance is credited automatically once payOS confirms the transfer.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                    disabled={busy}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='PayOSClientId'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Client ID</FormLabel>
                  <FormControl>
                    <Input autoComplete='off' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='PayOSApiKey'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>API Key</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      autoComplete='new-password'
                      placeholder={t('Leave empty to keep the saved key')}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='PayOSChecksumKey'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>Checksum Key</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      autoComplete='new-password'
                      placeholder={t('Leave empty to keep the saved key')}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='PayOSUnitPrice'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('VND per 1 unit of balance')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={0} step='1' {...field} />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Exchange rate charged for each unit topped up (e.g. 26000 = 26,000 ₫ per $1). 0 keeps payOS off.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='PayOSMinTopUp'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum top-up')}</FormLabel>
                  <FormControl>
                    <Input type='number' min={1} step='1' {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <Alert>
            <AlertDescription className='space-y-1'>
              <div>
                {t(
                  'In the payOS dashboard, set the payment channel webhook to:'
                )}
              </div>
              <code className='block break-all font-mono text-xs'>
                {webhookUrl}
              </code>
            </AlertDescription>
          </Alert>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
