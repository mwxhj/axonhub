'use client';

import { useState } from 'react';
import { CheckCircle2, ExternalLink } from 'lucide-react';
import type { FieldErrors, UseFormRegister, UseFormSetValue, UseFormWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { codexDecodeAuthJSON, codexOAuthExchange, codexOAuthStart } from '@/features/channels/data/codex';
import { claudecodeOAuthExchange, claudecodeOAuthStart } from '@/features/channels/data/claudecode';
import { antigravityOAuthExchange, antigravityOAuthStart } from '@/features/channels/data/antigravity';
import { CopilotDeviceFlow } from '@/features/channels/components/copilot-device-flow';
import { useOAuthFlow } from '@/features/channels/hooks/use-oauth-flow';
import type { CredentialFormValues, CredentialSecretMode, OAuthCredentialProvider, UpstreamCredential } from '../data/schema';

interface CredentialSecretFieldsProps {
  register: UseFormRegister<CredentialFormValues>;
  setValue: UseFormSetValue<CredentialFormValues>;
  watch: UseFormWatch<CredentialFormValues>;
  errors: FieldErrors<CredentialFormValues>;
  allowModeSwitch?: boolean;
  currentCredential?: Pick<UpstreamCredential, 'secretSummary'> | null;
  onImportedCredential?: (credential: UpstreamCredential) => void;
}

const oauthProviders: OAuthCredentialProvider[] = ['codex', 'claudecode', 'github_copilot', 'antigravity'];

function copilotCredentialsJSON(accessToken: string) {
  return JSON.stringify({
    access_token: accessToken,
    token_type: 'Bearer',
    scopes: ['read:user'],
  });
}

export function CredentialSecretFields({
  register,
  setValue,
  watch,
  errors,
  allowModeSwitch = true,
  currentCredential,
  onImportedCredential,
}: CredentialSecretFieldsProps) {
  const { t } = useTranslation();
  const [codexAuthJSONText, setCodexAuthJSONText] = useState('');
  const [isDecodingCodexAuthJSON, setIsDecodingCodexAuthJSON] = useState(false);
  const currentSecretKind = currentCredential?.secretSummary?.kind;
  const canSwitchMode = allowModeSwitch && !currentSecretKind;
  const secretMode = canSwitchMode ? watch('secretMode') : currentSecretKind === 'oauth' ? 'oauth' : 'api_key';
  const oauthProvider = watch('oauthProvider');
  const oauthCredentials = watch('oauthCredentials');

  const applyOAuthCredentials = (provider: OAuthCredentialProvider, credentials: string) => {
    setValue('secretMode', 'oauth', { shouldDirty: true, shouldValidate: true });
    setValue('oauthProvider', provider, { shouldDirty: true, shouldValidate: true });
    setValue('oauthCredentials', credentials, { shouldDirty: true, shouldValidate: true });
  };

  const handleImportedCredential = (credential: UpstreamCredential) => {
    onImportedCredential?.(credential);
  };

  const codexOAuth = useOAuthFlow({
    startFn: codexOAuthStart,
    exchangeFn: codexOAuthExchange,
    onSuccess: handleImportedCredential,
  });
  const claudecodeOAuth = useOAuthFlow({
    startFn: claudecodeOAuthStart,
    exchangeFn: claudecodeOAuthExchange,
    onSuccess: handleImportedCredential,
  });
  const antigravityOAuth = useOAuthFlow({
    startFn: antigravityOAuthStart,
    exchangeFn: antigravityOAuthExchange,
    onSuccess: handleImportedCredential,
  });

  const handleSecretModeChange = (value: string) => {
    setValue('secretMode', value as CredentialSecretMode, { shouldDirty: true, shouldValidate: true });
  };

  const handleDecodeCodexAuthJSON = async () => {
    if (!codexAuthJSONText.trim()) {
      return;
    }
    setIsDecodingCodexAuthJSON(true);
    try {
      const result = await codexDecodeAuthJSON({ auth_json: codexAuthJSONText });
      applyOAuthCredentials('codex', result.credentials);
      toast.success(t('credentials.oauth.messages.authJsonApplied'));
    } catch {
      toast.error(t('credentials.oauth.messages.authJsonInvalid'));
    } finally {
      setIsDecodingCodexAuthJSON(false);
    }
  };

  const renderRedirectOAuth = (
    oauth: ReturnType<typeof useOAuthFlow>,
    provider: OAuthCredentialProvider,
  ) => (
    <div className='grid gap-3 rounded-md border p-3'>
      <div className='flex flex-wrap gap-2'>
        <Button type='button' variant='secondary' onClick={oauth.start} disabled={oauth.isStarting}>
          {oauth.isStarting ? t('channels.dialogs.oauth.buttons.starting') : t('channels.dialogs.oauth.buttons.startOAuth')}
        </Button>
        {oauth.authUrl && (
          <Button type='button' variant='outline' onClick={() => window.open(oauth.authUrl || '', '_blank', 'noopener,noreferrer')}>
            <ExternalLink className='mr-2 h-4 w-4' />
            {t('channels.dialogs.oauth.buttons.openOAuthLink')}
          </Button>
        )}
      </div>

      {oauth.authUrl && (
        <div className='grid gap-2'>
          <Label>{t('channels.dialogs.oauth.labels.authorizationUrl')}</Label>
          <Input value={oauth.authUrl} readOnly className='bg-muted font-mono text-xs' />
        </div>
      )}

      <div className='grid gap-2'>
        <Label htmlFor={`credential-${provider}-callback-url`}>{t('channels.dialogs.oauth.labels.callbackUrl')}</Label>
        <Input
          id={`credential-${provider}-callback-url`}
          value={oauth.callbackUrl}
          onChange={(event) => oauth.setCallbackUrl(event.target.value)}
          placeholder={t('channels.dialogs.oauth.placeholders.callbackUrl')}
        />
      </div>

      <Button type='button' onClick={oauth.exchange} disabled={oauth.isExchanging || !oauth.sessionId}>
        {oauth.isExchanging ? t('channels.dialogs.oauth.buttons.exchanging') : t('credentials.oauth.buttons.importCredentials')}
      </Button>
    </div>
  );

  return (
    <div className='grid gap-4'>
      {canSwitchMode && (
        <Tabs value={secretMode} onValueChange={handleSecretModeChange} className='w-full'>
          <TabsList className='grid w-full grid-cols-2'>
            <TabsTrigger value='api_key'>{t('credentials.authKinds.api_key')}</TabsTrigger>
            <TabsTrigger value='oauth'>{t('credentials.authKinds.oauth')}</TabsTrigger>
          </TabsList>
        </Tabs>
      )}

      {secretMode === 'api_key' && (
        <div className='grid gap-2'>
          <Label htmlFor='credential-api-key'>{t('credentials.fields.apiKey')}</Label>
          <Input
            id='credential-api-key'
            type='password'
            autoComplete='off'
            {...register('apiKey', {
              validate: (value) =>
                secretMode !== 'api_key' || value.trim().length > 0 || t('credentials.validation.apiKeyRequired'),
            })}
          />
          {errors.apiKey && <span className='text-sm text-red-500'>{errors.apiKey.message}</span>}
        </div>
      )}

      {secretMode === 'oauth' && (
        <div className='grid gap-4'>
          <div className='grid gap-2'>
            <Label htmlFor='credential-oauth-provider'>{t('credentials.oauth.provider')}</Label>
            <Select
              value={oauthProvider}
              onValueChange={(value) => setValue('oauthProvider', value as OAuthCredentialProvider, { shouldDirty: true })}
            >
              <SelectTrigger id='credential-oauth-provider'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {oauthProviders.map((provider) => (
                  <SelectItem key={provider} value={provider}>
                    {t(`credentials.oauth.providers.${provider}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {oauthProvider === 'codex' && (
            <div className='grid gap-3 rounded-md border p-3'>
              <div className='grid gap-2'>
                <Label htmlFor='credential-codex-auth-json'>{t('credentials.oauth.codexAuthJson.label')}</Label>
                <Textarea
                  id='credential-codex-auth-json'
                  rows={4}
                  value={codexAuthJSONText}
                  onChange={(event) => setCodexAuthJSONText(event.target.value)}
                  placeholder={t('credentials.oauth.codexAuthJson.placeholder')}
                />
              </div>
              <Button type='button' variant='secondary' onClick={handleDecodeCodexAuthJSON} disabled={isDecodingCodexAuthJSON}>
                {isDecodingCodexAuthJSON ? t('common.loading') : t('credentials.oauth.buttons.applyAuthJson')}
              </Button>
            </div>
          )}

          {oauthProvider === 'codex' && renderRedirectOAuth(codexOAuth, 'codex')}
          {oauthProvider === 'claudecode' && renderRedirectOAuth(claudecodeOAuth, 'claudecode')}
          {oauthProvider === 'antigravity' && renderRedirectOAuth(antigravityOAuth, 'antigravity')}
          {oauthProvider === 'github_copilot' && (
            <CopilotDeviceFlow
              onSuccess={handleImportedCredential}
              hasExistingCredential={currentSecretKind === 'oauth' && oauthProvider === 'github_copilot'}
            />
          )}

          <div className='grid gap-2'>
            <Label htmlFor='credential-oauth-credentials'>{t('credentials.oauth.credentials')}</Label>
            <Textarea
              id='credential-oauth-credentials'
              rows={4}
              className='font-mono text-xs'
              {...register('oauthCredentials', {
                validate: (value) =>
                  secretMode !== 'oauth' || value.trim().length > 0 || t('credentials.validation.oauthAccessTokenRequired'),
              })}
            />
            {oauthCredentials && (
              <div className='text-muted-foreground flex items-center gap-2 text-xs'>
                <CheckCircle2 className='h-3.5 w-3.5' />
                {t('credentials.oauth.messages.credentialsReady')}
              </div>
            )}
            {errors.oauthCredentials && <span className='text-sm text-red-500'>{errors.oauthCredentials.message}</span>}
          </div>
        </div>
      )}
    </div>
  );
}
