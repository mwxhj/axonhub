'use client';

import { useMemo, useState } from 'react';
import { Link, Unlink } from 'lucide-react';
import { useTranslation } from 'react-i18next';
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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import {
  useAttachCredentialToChannel,
  useDetachCredentialFromChannel,
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
  const [weightOverride, setWeightOverride] = useState(refItem.weightOverride?.toString() ?? '');

  const handleSaveWeight = async () => {
    const trimmed = weightOverride.trim();
    await updateRef.mutateAsync({
      id: refItem.id,
      input: trimmed ? { weightOverride: Number(trimmed) } : { clearWeightOverride: true },
    });
  };

  return (
    <div className='grid gap-3 border-b py-3 last:border-b-0 md:grid-cols-[1fr_120px_150px_92px] md:items-center'>
      <div className='min-w-0'>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='truncate font-medium'>{credential.name || credential.fingerprint}</span>
          <Badge variant={credential.status === 'enabled' ? 'default' : 'secondary'}>{t(`credentials.status.${credential.status}`)}</Badge>
        </div>
        <div className='text-muted-foreground mt-1 truncate text-xs'>
          {t(`credentials.authKinds.${credential.secretKind}`)} · {credential.keyHint || credential.issuerScope || '-'}
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

      <div className='flex items-center gap-2'>
        <Input
          type='number'
          min={1}
          value={weightOverride}
          placeholder={t('credentials.fields.inheritWeight')}
          onChange={(event) => setWeightOverride(event.target.value)}
        />
        <Button type='button' variant='outline' size='sm' disabled={updateRef.isPending} onClick={handleSaveWeight}>
          {t('common.buttons.save')}
        </Button>
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
  const [credentialID, setCredentialID] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [weightOverride, setWeightOverride] = useState('');

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

  const handleAttach = async () => {
    if (!credentialID) {
      return;
    }
    const trimmedWeight = weightOverride.trim();
    await attach.mutateAsync({
      channelID: channel.id,
      credentialID,
      enabled,
      weightOverride: trimmedWeight ? Number(trimmedWeight) : undefined,
    });
    setCredentialID('');
    setEnabled(true);
    setWeightOverride('');
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-[900px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.channels.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.channels.description')}</DialogDescription>
        </DialogHeader>

        <div className='grid max-h-[72vh] gap-5 overflow-y-auto py-2 pr-1'>
          <div className='grid gap-3 rounded-md border p-3'>
            <div className='grid grid-cols-1 gap-3 md:grid-cols-[1fr_120px_150px_auto] md:items-end'>
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
              <div className='grid gap-2'>
                <Label htmlFor='channel-credential-weight'>{t('credentials.fields.weightOverride')}</Label>
                <Input
                  id='channel-credential-weight'
                  type='number'
                  min={1}
                  value={weightOverride}
                  placeholder={t('credentials.fields.inheritWeight')}
                  onChange={(event) => setWeightOverride(event.target.value)}
                />
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
