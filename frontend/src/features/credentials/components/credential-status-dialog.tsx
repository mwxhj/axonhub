'use client';

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
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
import { useCredentialsContext } from '../context/credentials-context';
import { useUpdateUpstreamCredentialStatus } from '../data/credentials';
import type { CredentialStatus } from '../data/schema';

const statuses: CredentialStatus[] = ['enabled', 'disabled', 'archived'];

export function CredentialStatusDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const updateStatus = useUpdateUpstreamCredentialStatus();
  const isOpen = open === 'status' && !!currentCredential;
  const [status, setStatus] = useState<CredentialStatus>('enabled');

  useEffect(() => {
    if (isOpen && currentCredential) {
      setStatus(currentCredential.status);
    }
  }, [currentCredential, isOpen]);

  const close = () => {
    setOpen(null);
    setCurrentCredential(null);
  };

  const handleSubmit = async () => {
    if (!currentCredential) {
      return;
    }

    await updateStatus.mutateAsync({
      id: currentCredential.id,
      status,
    });
    close();
  };

  return (
    <Dialog open={isOpen} onOpenChange={(nextOpen) => (nextOpen ? setOpen('status') : close())}>
      <DialogContent className='sm:max-w-[480px]'>
        <DialogHeader>
          <DialogTitle>{t('credentials.dialogs.status.title')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.status.description', { name: currentCredential?.name || currentCredential?.fingerprint || '' })}</DialogDescription>
        </DialogHeader>
        <div className='grid gap-2 py-4'>
          <Label htmlFor='credential-status-select'>{t('credentials.fields.status')}</Label>
          <Select value={status} onValueChange={(value) => setStatus(value as CredentialStatus)}>
            <SelectTrigger id='credential-status-select'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {statuses.map((item) => (
                <SelectItem key={item} value={item}>
                  {t(`credentials.status.${item}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <DialogFooter>
          <Button type='button' variant='outline' onClick={close}>
            {t('common.buttons.cancel')}
          </Button>
          <Button type='button' disabled={updateStatus.isPending || status === currentCredential?.status} onClick={handleSubmit}>
            {updateStatus.isPending ? t('common.buttons.saving') : t('common.buttons.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
