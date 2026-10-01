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

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'
import { ReferralCommissionStats } from './referral-commission-stats'

// Hoa hồng giới thiệu theo tiền nạp: mỗi lần một người nạp tiền thật, người đã
// mời họ nhận X% hạn mức vừa nạp. 0 = tắt. Bên dưới là thống kê toàn hệ thống.

const schema = z.object({
  ReferralCommissionPercent: z.coerce.number().min(0).max(100),
})

type Values = z.infer<typeof schema>

export function ReferralCommissionSection(props: {
  defaultValues: Values
  complianceConfirmed: boolean
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: props.defaultValues,
  })
  const { isDirty, isSubmitting } = form.formState

  async function onSubmit(values: Values) {
    if (
      values.ReferralCommissionPercent ===
      form.formState.defaultValues?.ReferralCommissionPercent
    ) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOption.mutateAsync({
      key: 'ReferralCommissionPercent',
      value: String(values.ReferralCommissionPercent),
    })
    form.reset(values)
  }

  return (
    <SettingsSection title={t('Referral commission')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save commission rate'
          />

          {!props.complianceConfirmed && (
            <Alert>
              <AlertDescription>
                {t(
                  'Referral commission can only be turned on after the payment compliance terms are confirmed in Payment Gateway.'
                )}
              </AlertDescription>
            </Alert>
          )}

          <FormField
            control={form.control}
            name='ReferralCommissionPercent'
            render={({ field }) => (
              <FormItem className='max-w-sm'>
                <FormLabel>{t('Commission rate (%)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={100}
                    step='0.1'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Share of every paid top-up that goes to the user who invited the payer, added to their referral rewards. 0 turns it off. Redemption codes and manual balance edits earn nothing.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>

      <ReferralCommissionStats />
    </SettingsSection>
  )
}
