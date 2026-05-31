'use client';

import { useEffect, useMemo, useState } from 'react';
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
import { useCredentialsContext } from '../context/credentials-context';
import {
  useAttachCredentialToChannel,
  useAttachableChannels,
  useDetachCredentialFromChannel,
  useUpdateChannelCredentialRef,
  type CredentialRef,
} from '../data/credentials';

function RefRow({ refItem }: { refItem: CredentialRef }) {
  const { t } = useTranslation();
  const updateRef = useUpdateChannelCredentialRef();
  const detach = useDetachCredentialFromChannel();
  const [weightOverride, setWeightOverride] = useState(refItem.weightOverride?.toString() ?? '');

  useEffect(() => {
    setWeightOverride(refItem.weightOverride?.toString() ?? '');
  }, [refItem.weightOverride]);

  const handleToggle = async (enabled: boolean) => {
    await updateRef.mutateAsync({
      id: refItem.id,
      input: { enabled },
    });
  };

  const handleSaveWeight = async () => {
    const trimmed = weightOverride.trim();
    await updateRef.mutateAsync({
      id: refItem.id,
      input: trimmed
        ? {
            weightOverride: Number(trimmed),
          }
        : {
            clearWeightOverride: true,
          },
    });
  };

  const handleDetach = async () => {
    await detach.mutateAsync({
      channelID: refItem.channelID,
      credentialID: refItem.credentialID,
    });
  };

  return (
    <div className='grid gap-3 border-b py-3 last:border-b-0 md:grid-cols-[1fr_120px_150px_92px] md:items-center'>
      <div className='min-w-0'>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='truncate font-medium'>{refItem.channel?.name ?? refItem.channelID}</span>
          {refItem.channel?.status && (
            <Badge variant={refItem.channel.status === 'enabled' ? 'default' : 'secondary'}>
              {t(`credentials.channelStatus.${refItem.channel.status}`)}
            </Badge>
          )}
        </div>
        <div className='text-muted-foreground mt-1 truncate text-xs'>
          {refItem.channel?.type ?? '-'} · {refItem.channel?.baseURL || '-'}
        </div>
      </div>

      <div className='flex items-center gap-2'>
        <Switch checked={refItem.enabled} onCheckedChange={handleToggle} disabled={updateRef.isPending} />
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

      <Button type='button' variant='outline' size='sm' disabled={detach.isPending} onClick={handleDetach}>
        <Unlink className='mr-2 h-4 w-4' />
        {t('credentials.dialogs.channels.detach')}
      </Button>
    </div>
  );
}

export function CredentialChannelsDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const attach = useAttachCredentialToChannel();
  const isOpen = open === 'channels' && !!currentCredential;
  const [channelID, setChannelID] = useState('');
  const [enabled, setEnabled] = useState(true);
  const [weightOverride, setWeightOverride] = useState('');

  const refs = useMemo(
    () => currentCredential?.channelRefs?.edges?.map((edge) => edge.node).filter((node): node is CredentialRef => Boolean(node)) ?? [],
    [currentCredential?.channelRefs?.edges]
  );

  const attachedChannelIDs = useMemo(() => new Set(refs.map((ref) => ref.channelID)), [refs]);
  const { data: channels } = useAttachableChannels(
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
    { enabled: isOpen }
  );

  const attachableChannels = useMemo(
    () =>
      channels?.edges?.map((edge) => edge.node).filter((node): node is NonNullable<typeof node> => {
        if (!node) {
          return false;
        }
        return !attachedChannelIDs.has(node.id);
      }) ?? [],
    [attachedChannelIDs, channels?.edges]
  );

  useEffect(() => {
    if (isOpen) {
      setChannelID('');
      setEnabled(true);
      setWeightOverride('');
    }
  }, [isOpen]);

  const close = () => {
    setOpen(null);
    setCurrentCredential(null);
  };

  const handleAttach = async () => {
    if (!currentCredential || !channelID) {
      return;
    }

    const trimmedWeight = weightOverride.trim();
    await attach.mutateAsync({
      channelID,
      credentialID: currentCredential.id,
      enabled,
      weightOverride: trimmedWeight ? Number(trimmedWeight) : undefined,
    });
    setChannelID('');
    setEnabled(true);
    setWeightOverride('');
  };

  return (
    <Dialog open={isOpen} onOpenChange={(nextOpen) => (nextOpen ? setOpen('channels') : close())}>
      <DialogContent className='sm:max-w-[900px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.channels.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.channels.description')}</DialogDescription>
        </DialogHeader>

        <div className='grid max-h-[72vh] gap-5 overflow-y-auto py-2 pr-1'>
          <div className='grid gap-3 rounded-md border p-3'>
            <div className='grid grid-cols-1 gap-3 md:grid-cols-[1fr_120px_150px_auto] md:items-end'>
              <div className='grid gap-2'>
                <Label htmlFor='attach-channel'>{t('credentials.fields.channel')}</Label>
                <Select value={channelID} onValueChange={setChannelID}>
                  <SelectTrigger id='attach-channel'>
                    <SelectValue placeholder={t('credentials.dialogs.channels.selectChannel')} />
                  </SelectTrigger>
                  <SelectContent>
                    {attachableChannels.map((channel) => (
                      <SelectItem key={channel.id} value={channel.id}>
                        {channel.name}
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
                <Label htmlFor='attach-weight'>{t('credentials.fields.weightOverride')}</Label>
                <Input
                  id='attach-weight'
                  type='number'
                  min={1}
                  value={weightOverride}
                  placeholder={t('credentials.fields.inheritWeight')}
                  onChange={(event) => setWeightOverride(event.target.value)}
                />
              </div>
              <Button type='button' disabled={!channelID || attach.isPending} onClick={handleAttach}>
                <Link className='mr-2 h-4 w-4' />
                {t('credentials.dialogs.channels.attach')}
              </Button>
            </div>
          </div>

          <div className='rounded-md border px-3'>
            {refs.length > 0 ? (
              refs.map((refItem) => <RefRow key={refItem.id} refItem={refItem} />)
            ) : (
              <div className='text-muted-foreground py-8 text-center text-sm'>{t('credentials.dialogs.channels.empty')}</div>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button type='button' variant='outline' onClick={close}>
            {t('common.buttons.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
