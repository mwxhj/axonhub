'use client';

import type { FieldErrors, UseFormRegister } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import type { CredentialAuthKind, CredentialFormValues } from '../data/schema';

interface CredentialSecretFieldsProps {
  authKind: CredentialAuthKind;
  register: UseFormRegister<CredentialFormValues>;
  errors: FieldErrors<CredentialFormValues>;
}

export function CredentialSecretFields({ authKind, register, errors }: CredentialSecretFieldsProps) {
  const { t } = useTranslation();

  if (authKind === 'oauth') {
    return (
      <>
        <div className='grid gap-2'>
          <Label htmlFor='credential-oauth-access-token'>{t('credentials.fields.oauthAccessToken')}</Label>
          <Input
            id='credential-oauth-access-token'
            type='password'
            autoComplete='off'
            {...register('oauthAccessToken', {
              validate: (value, formValues) =>
                formValues.authKind !== 'oauth' || value.trim().length > 0 || t('credentials.validation.oauthAccessTokenRequired'),
            })}
          />
          {errors.oauthAccessToken && <span className='text-sm text-red-500'>{errors.oauthAccessToken.message}</span>}
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='credential-oauth-refresh-token'>{t('credentials.fields.oauthRefreshToken')}</Label>
          <Input id='credential-oauth-refresh-token' type='password' autoComplete='off' {...register('oauthRefreshToken')} />
        </div>
        <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='credential-oauth-client-id'>{t('credentials.fields.oauthClientID')}</Label>
            <Input id='credential-oauth-client-id' {...register('oauthClientID')} />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='credential-oauth-token-type'>{t('credentials.fields.oauthTokenType')}</Label>
            <Input id='credential-oauth-token-type' placeholder='Bearer' {...register('oauthTokenType')} />
          </div>
        </div>
        <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='credential-oauth-expires-at'>{t('credentials.fields.oauthExpiresAt')}</Label>
            <Input id='credential-oauth-expires-at' type='datetime-local' {...register('oauthExpiresAt')} />
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='credential-oauth-scopes'>{t('credentials.fields.oauthScopes')}</Label>
            <Input id='credential-oauth-scopes' placeholder='scope:a, scope:b' {...register('oauthScopes')} />
          </div>
        </div>
      </>
    );
  }

  if (authKind === 'gcp') {
    return (
      <>
        <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
          <div className='grid gap-2'>
            <Label htmlFor='credential-gcp-region'>{t('credentials.fields.gcpRegion')}</Label>
            <Input
              id='credential-gcp-region'
              {...register('gcpRegion', {
                validate: (value, formValues) =>
                  formValues.authKind !== 'gcp' || value.trim().length > 0 || t('credentials.validation.gcpRegionRequired'),
              })}
            />
            {errors.gcpRegion && <span className='text-sm text-red-500'>{errors.gcpRegion.message}</span>}
          </div>
          <div className='grid gap-2'>
            <Label htmlFor='credential-gcp-project-id'>{t('credentials.fields.gcpProjectID')}</Label>
            <Input
              id='credential-gcp-project-id'
              {...register('gcpProjectID', {
                validate: (value, formValues) =>
                  formValues.authKind !== 'gcp' || value.trim().length > 0 || t('credentials.validation.gcpProjectIDRequired'),
              })}
            />
            {errors.gcpProjectID && <span className='text-sm text-red-500'>{errors.gcpProjectID.message}</span>}
          </div>
        </div>
        <div className='grid gap-2'>
          <Label htmlFor='credential-gcp-json'>{t('credentials.fields.gcpJSONData')}</Label>
          <Textarea
            id='credential-gcp-json'
            rows={8}
            className='font-mono text-sm'
            {...register('gcpJSONData', {
              validate: (value, formValues) =>
                formValues.authKind !== 'gcp' || value.trim().length > 0 || t('credentials.validation.gcpJSONDataRequired'),
            })}
          />
          {errors.gcpJSONData && <span className='text-sm text-red-500'>{errors.gcpJSONData.message}</span>}
        </div>
      </>
    );
  }

  if (authKind === 'azure') {
    return (
      <div className='grid gap-2'>
        <Label htmlFor='credential-azure-api-version'>{t('credentials.fields.azureAPIVersion')}</Label>
        <Input
          id='credential-azure-api-version'
          placeholder='2024-02-15-preview'
          {...register('azureAPIVersion', {
            validate: (value, formValues) =>
              formValues.authKind !== 'azure' || value.trim().length > 0 || t('credentials.validation.azureAPIVersionRequired'),
          })}
        />
        {errors.azureAPIVersion && <span className='text-sm text-red-500'>{errors.azureAPIVersion.message}</span>}
      </div>
    );
  }

  return (
    <div className='grid gap-2'>
      <Label htmlFor='credential-api-key'>{t('credentials.fields.apiKey')}</Label>
      <Input
        id='credential-api-key'
        type='password'
        autoComplete='off'
        {...register('apiKey', {
          validate: (value, formValues) =>
            (formValues.authKind !== 'api_key' && formValues.authKind !== 'other') ||
            value.trim().length > 0 ||
            t('credentials.validation.apiKeyRequired'),
        })}
      />
      {errors.apiKey && <span className='text-sm text-red-500'>{errors.apiKey.message}</span>}
    </div>
  );
}
