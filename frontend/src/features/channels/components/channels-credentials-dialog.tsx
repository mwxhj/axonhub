'use client';

import { useMemo, useState } from 'react';
import { AlertTriangle, Link, Unlink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import {
  useAttachCredentialToChannel,
  useDetachCredentialFromChannel,
  useMigrateLegacyChannelCredentials,
  useUpdateChannelCredentialRef,
  useUpstreamCredentials,
  type CredentialRef,
  type UpstreamCredential,
} from '@/features/credentials/data/credentials';
import type { Channel } from '../data/schema';

interface ChannelsCredentialsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  channel: Channel;
}

function getRefForChannel(credential: UpstreamCredential, channelID: string) {
  return credential.channelRefs?.edges?.map((edge) => edge.node).find((ref): ref is CredentialRef => {
    if (!ref) {
      return false;
    }
    return ref.channelID === channelID;
  });
}

function ChannelCredentialRow({ credential, refItem }: { credential: UpstreamCredential; refItem: CredentialRef }) {
  const { t } = useTranslation();
  const updateRef = useUpdateChannelCredentialRef();
  const detach = useDetachCredentialFromChannel();
  const quotaStatus = credential.quotaScope?.status || credential.quotaStatus || 'unknown';

  return (
    <div className='grid gap-3 border-b py-3 last:border-b-0 md:grid-cols-[1fr_120px_92px] md:items-center'>
      <div className='min-w-0'>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='truncate font-medium'>{credential.name || credential.fingerprint}</span>
          <Badge variant={credential.status === 'enabled' ? 'default' : 'secondary'}>{t(`credentials.status.${credential.status}`)}</Badge>
          <Badge variant='outline'>{t(`credentials.quota.status.${quotaStatus}`, { defaultValue: quotaStatus })}</Badge>
        </div>
        <div className='text-muted-foreground mt-1 truncate text-xs'>
          {credential.keyHint || '-'}
          {credential.quotaScope?.name ? ` · ${credential.quotaScope.name}` : ''}
        </div>
      </div>

      <div className='flex items-center gap-2'>
        <Switch
          checked={refItem.enabled}
          disabled={updateRef.isPending}
          onCheckedChange={(enabled) => updateRef.mutate({ id: refItem.id, input: { enabled } })}
        />
        <span className='text-sm'>{refItem.enabled ? t('credentials.fields.enabled') : t('credentials.fields.disabled')}</span>
      </div>

      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={detach.isPending}
        onClick={() => detach.mutate({ channelID: refItem.channelID, credentialID: refItem.credentialID })}
      >
        <Unlink className='mr-2 h-4 w-4' />
        {t('credentials.dialogs.channels.detach')}
      </Button>
    </div>
  );
}

export function ChannelsCredentialsDialog({ open, onOpenChange, channel }: ChannelsCredentialsDialogProps) {
  const { t } = useTranslation();
  const attach = useAttachCredentialToChannel();
  const migrateLegacy = useMigrateLegacyChannelCredentials();
  const [credentialID, setCredentialID] = useState('');
  const [enabled, setEnabled] = useState(true);

  const { data } = useUpstreamCredentials(
    {
      first: 200,
      where: {
        statusIn: ['enabled', 'disabled'],
      },
      orderBy: {
        field: 'NAME',
        direction: 'ASC',
      },
    },
    { enabled: open }
  );

  const credentials = useMemo(
    () => data?.edges?.map((edge) => edge.node).filter((node): node is UpstreamCredential => Boolean(node)) ?? [],
    [data?.edges]
  );

  const attachedCredentials = useMemo(
    () =>
      credentials
        .map((credential) => ({ credential, refItem: getRefForChannel(credential, channel.id) }))
        .filter((item): item is { credential: UpstreamCredential; refItem: CredentialRef } => Boolean(item.refItem)),
    [channel.id, credentials]
  );

  const attachableCredentials = useMemo(
    () => credentials.filter((credential) => !getRefForChannel(credential, channel.id)),
    [channel.id, credentials]
  );

  const hasLegacyInlineCredentials = useMemo(() => {
    const legacyApiKey = channel.credentials?.apiKey?.trim();
    const legacyAPIKeys = channel.credentials?.apiKeys?.some((key) => key.trim().length > 0);

    return Boolean(legacyApiKey || legacyAPIKeys);
  }, [channel.credentials?.apiKey, channel.credentials?.apiKeys]);

  const handleAttach = async () => {
    if (!credentialID) {
      return;
    }
    await attach.mutateAsync({
      channelID: channel.id,
      credentialID,
      enabled,
    });
    setCredentialID('');
    setEnabled(true);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-[900px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.channels.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.channels.description')}</DialogDescription>
        </DialogHeader>

        <div className='grid max-h-[72vh] gap-5 overflow-y-auto py-2 pr-1'>
          {hasLegacyInlineCredentials && (
            <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-200'>
              <AlertTriangle className='h-4 w-4' />
              <AlertDescription className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                <span>{t('credentials.dialogs.channels.legacyInlineWarning')}</span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  disabled={migrateLegacy.isPending}
                  onClick={() => migrateLegacy.mutate()}
                >
                  {t('credentials.buttons.migrateLegacy')}
                </Button>
              </AlertDescription>
            </Alert>
          )}

          <div className='grid gap-3 rounded-md border p-3'>
            <div className='grid grid-cols-1 gap-3 md:grid-cols-[1fr_120px_auto] md:items-end'>
              <div className='grid gap-2'>
                <Label htmlFor='channel-credential'>{t('credentials.fields.name')}</Label>
                <Select value={credentialID} onValueChange={setCredentialID}>
                  <SelectTrigger id='channel-credential'>
                    <SelectValue placeholder={t('credentials.dialogs.channels.selectCredential')} />
                  </SelectTrigger>
                  <SelectContent>
                    {attachableCredentials.map((credential) => (
                      <SelectItem key={credential.id} value={credential.id}>
                        {credential.name || credential.fingerprint}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className='flex items-center gap-2 pb-2'>
                <Switch checked={enabled} onCheckedChange={setEnabled} />
                <Label>{enabled ? t('credentials.fields.enabled') : t('credentials.fields.disabled')}</Label>
              </div>
              <Button type='button' disabled={!credentialID || attach.isPending} onClick={handleAttach}>
                <Link className='mr-2 h-4 w-4' />
                {t('credentials.dialogs.channels.attach')}
              </Button>
            </div>
          </div>

          <div className='rounded-md border px-3'>
            {attachedCredentials.length > 0 ? (
              attachedCredentials.map(({ credential, refItem }) => (
                <ChannelCredentialRow key={refItem.id} credential={credential} refItem={refItem} />
              ))
            ) : (
              <div className='text-muted-foreground py-8 text-center text-sm'>{t('credentials.dialogs.channels.empty')}</div>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={() => onOpenChange(false)}>
            {t('common.buttons.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
