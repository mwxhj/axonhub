'use client';

import type { FieldErrors, UseFormRegister, UseFormSetValue, UseFormWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { useCredentialQuotaScopes } from '../data/credentials';
import type {
  CredentialFormValues,
  CredentialQuotaOverLimitAction,
  CredentialQuotaResetPolicy,
  CredentialQuotaScope,
  CredentialQuotaUnit,
} from '../data/schema';

interface CredentialQuotaFieldsProps {
  register: UseFormRegister<CredentialFormValues>;
  setValue: UseFormSetValue<CredentialFormValues>;
  watch: UseFormWatch<CredentialFormValues>;
  errors: FieldErrors<CredentialFormValues>;
}

const quotaUnits: CredentialQuotaUnit[] = ['usd', 'token', 'request', 'credit', 'custom', 'unknown'];
const resetPolicies: CredentialQuotaResetPolicy[] = ['none', 'manual', 'daily', 'monthly', 'custom'];
const overLimitActions: CredentialQuotaOverLimitAction[] = ['warn', 'pause', 'disable'];
const automaticUnits: CredentialQuotaUnit[] = ['usd', 'token', 'request'];

function isQuotaScope(scope: CredentialQuotaScope | null | undefined): scope is CredentialQuotaScope {
  return Boolean(scope);
}

function dateTimeLocalValue(date: Date) {
  const pad = (value: number) => String(value).padStart(2, '0');
  return [
    date.getFullYear(),
    '-',
    pad(date.getMonth() + 1),
    '-',
    pad(date.getDate()),
    'T',
    pad(date.getHours()),
    ':',
    pad(date.getMinutes()),
  ].join('');
}

function nextResetValue(policy: CredentialQuotaResetPolicy) {
  const now = new Date();
  if (policy === 'custom') {
    return dateTimeLocalValue(new Date(now.getTime() + 24 * 60 * 60 * 1000));
  }
  return '';
}

export function CredentialQuotaFields({ register, setValue, watch, errors }: CredentialQuotaFieldsProps) {
  const { t } = useTranslation();
  const quotaScopesQuery = useCredentialQuotaScopes({ first: 100 });
  const quotaScopes = quotaScopesQuery.data?.edges?.map((edge) => edge.node).filter(isQuotaScope) ?? [];
  const quotaScopeMode = watch('quotaScopeMode');
  const quotaScopeID = watch('quotaScopeID');
  const unit = watch('quotaUnit');
  const resetPolicy = watch('quotaResetPolicy');
  const resetAt = watch('quotaResetAt');
  const windowStartedAt = watch('quotaWindowStartedAt');
  const overLimitAction = watch('quotaOverLimitAction');
  const unitBehaviorKey = automaticUnits.includes(unit) ? 'automatic' : 'manual';

  const handleResetPolicyChange = (value: string) => {
    const policy = value as CredentialQuotaResetPolicy;
    setValue('quotaResetPolicy', policy);
    if (policy === 'none' || policy === 'manual') {
      setValue('quotaResetAt', '');
      setValue('quotaWindowStartedAt', '');
      return;
    }
    if (policy === 'custom' && !resetAt) {
      setValue('quotaResetAt', nextResetValue(policy));
    }
    if (!windowStartedAt) {
      setValue('quotaWindowStartedAt', dateTimeLocalValue(new Date()));
    }
  };

  return (
    <div className='grid gap-4 rounded-md border p-3'>
      <div className='grid gap-1'>
        <h4 className='text-sm font-medium'>{t('credentials.quota.localTitle')}</h4>
        <p className='text-muted-foreground text-xs'>{t('credentials.quota.description')}</p>
      </div>

      <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
        <div className='grid gap-2'>
          <Label htmlFor='credential-quota-mode'>{t('credentials.fields.quotaScopeMode')}</Label>
          <Select
            value={quotaScopeMode}
            onValueChange={(value) => setValue('quotaScopeMode', value as CredentialFormValues['quotaScopeMode'])}
          >
            <SelectTrigger id='credential-quota-mode'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='none'>{t('credentials.quota.modes.none')}</SelectItem>
              <SelectItem value='new'>{t('credentials.quota.modes.new')}</SelectItem>
              <SelectItem value='shared'>{t('credentials.quota.modes.shared')}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {quotaScopeMode === 'shared' && (
          <div className='grid gap-2'>
            <Label htmlFor='credential-quota-scope'>{t('credentials.fields.quotaScope')}</Label>
            <Select value={quotaScopeID || undefined} onValueChange={(value) => setValue('quotaScopeID', value)}>
              <SelectTrigger id='credential-quota-scope'>
                <SelectValue placeholder={t('credentials.quota.selectSharedScope')} />
              </SelectTrigger>
              <SelectContent>
                {quotaScopes.map((scope) => (
                  <SelectItem key={scope.id} value={scope.id}>
                    {scope.name?.trim() || t('credentials.quota.unnamedScope')}
                    {scope.status ? ` · ${t(`credentials.quota.status.${scope.status}`)}` : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
      </div>

      {quotaScopeMode !== 'new' && (
        <p className='text-muted-foreground text-xs'>
          {quotaScopeMode === 'shared' ? t('credentials.quota.sharedScopeHint') : t('credentials.quota.noneScopeHint')}
        </p>
      )}

      {quotaScopeMode === 'new' && (
        <>
          <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-name'>{t('credentials.fields.quotaScopeName')}</Label>
              <Input id='credential-quota-name' {...register('quotaScopeName')} />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-unit'>{t('credentials.fields.quotaUnit')}</Label>
              <Select value={unit} onValueChange={(value) => setValue('quotaUnit', value as CredentialQuotaUnit)}>
                <SelectTrigger id='credential-quota-unit'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {quotaUnits.map((item) => (
                    <SelectItem key={item} value={item}>
                      {t(`credentials.quota.units.${item}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <span className='text-muted-foreground text-xs'>{t(`credentials.quota.unitBehavior.${unitBehaviorKey}`)}</span>
            </div>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-limit'>{t('credentials.fields.quotaLimitAmount')}</Label>
              <Input id='credential-quota-limit' inputMode='decimal' {...register('quotaLimitAmount')} />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-used'>{t('credentials.fields.quotaUsedAmount')}</Label>
              <Input id='credential-quota-used' inputMode='decimal' {...register('quotaUsedAmount')} />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-warning'>{t('credentials.fields.quotaWarningThresholdPercent')}</Label>
              <Input
                id='credential-quota-warning'
                type='number'
                min={0}
                max={100}
                {...register('quotaWarningThresholdPercent', {
                  valueAsNumber: true,
                  min: { value: 0, message: t('credentials.validation.quotaThresholdRange') },
                  max: { value: 100, message: t('credentials.validation.quotaThresholdRange') },
                })}
              />
              {errors.quotaWarningThresholdPercent && (
                <span className='text-sm text-red-500'>{errors.quotaWarningThresholdPercent.message}</span>
              )}
            </div>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-reset'>{t('credentials.fields.quotaResetPolicy')}</Label>
              <Select value={resetPolicy} onValueChange={handleResetPolicyChange}>
                <SelectTrigger id='credential-quota-reset'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {resetPolicies.map((item) => (
                    <SelectItem key={item} value={item}>
                      {t(`credentials.quota.resetPolicies.${item}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-reset-at'>{t('credentials.fields.quotaResetAt')}</Label>
              <Input id='credential-quota-reset-at' type='datetime-local' {...register('quotaResetAt')} />
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-window-start'>{t('credentials.fields.quotaWindowStartedAt')}</Label>
              <Input id='credential-quota-window-start' type='datetime-local' {...register('quotaWindowStartedAt')} />
            </div>
          </div>

          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            <div className='grid gap-2'>
              <Label htmlFor='credential-quota-action'>{t('credentials.fields.quotaOverLimitAction')}</Label>
              <Select
                value={overLimitAction}
                onValueChange={(value) => setValue('quotaOverLimitAction', value as CredentialQuotaOverLimitAction)}
              >
                <SelectTrigger id='credential-quota-action'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {overLimitActions.map((item) => (
                    <SelectItem key={item} value={item}>
                      {t(`credentials.quota.overLimitActions.${item}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          <div className='grid gap-2'>
            <Label htmlFor='credential-quota-remark'>{t('credentials.fields.quotaRemark')}</Label>
            <Textarea id='credential-quota-remark' rows={2} {...register('quotaRemark')} />
          </div>
        </>
      )}
    </div>
  );
}
