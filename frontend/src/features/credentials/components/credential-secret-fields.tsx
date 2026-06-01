'use client';

import type { FieldErrors, UseFormRegister } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import type { CredentialFormValues } from '../data/schema';

interface CredentialSecretFieldsProps {
  register: UseFormRegister<CredentialFormValues>;
  errors: FieldErrors<CredentialFormValues>;
}

export function CredentialSecretFields({ register, errors }: CredentialSecretFieldsProps) {
  const { t } = useTranslation();

  return (
    <div className='grid gap-2'>
      <Label htmlFor='credential-api-key'>{t('credentials.fields.apiKey')}</Label>
      <Input
        id='credential-api-key'
        type='password'
        autoComplete='off'
        {...register('apiKey', {
          validate: (value) => value.trim().length > 0 || t('credentials.validation.apiKeyRequired'),
        })}
      />
      {errors.apiKey && <span className='text-sm text-red-500'>{errors.apiKey.message}</span>}
    </div>
  );
}
