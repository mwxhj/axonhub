'use client';

import { Archive } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { ConfirmDialog } from '@/components/confirm-dialog';
import { useCredentialsContext } from '../context/credentials-context';
import { useArchiveUpstreamCredential } from '../data/credentials';

export function CredentialArchiveDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const archiveMutation = useArchiveUpstreamCredential();
  const isOpen = open === 'archive' && !!currentCredential;

  const close = () => {
    setOpen(null);
    setCurrentCredential(null);
  };

  const handleConfirm = async () => {
    if (!currentCredential) {
      return;
    }

    await archiveMutation.mutateAsync(currentCredential.id);
    close();
  };

  return (
    <ConfirmDialog
      open={isOpen}
      onOpenChange={(nextOpen) => (nextOpen ? setOpen('archive') : close())}
      title={
        <span className='flex items-center gap-2'>
          <Archive className='h-4 w-4' />
          {t('credentials.dialogs.archive.title')}
        </span>
      }
      desc={
        <div className='space-y-3'>
          <div>
            {t('credentials.dialogs.archive.description', {
              name: currentCredential?.name?.trim() || t('credentials.unnamed'),
            })}
          </div>
          <ul className='text-muted-foreground list-disc space-y-1 pl-5 text-sm'>
            <li>{t('credentials.dialogs.archive.effects.routing')}</li>
            <li>{t('credentials.dialogs.archive.effects.refs')}</li>
            <li>{t('credentials.dialogs.archive.effects.history')}</li>
            <li>{t('credentials.dialogs.archive.effects.secret')}</li>
          </ul>
        </div>
      }
      cancelBtnText={t('common.buttons.cancel')}
      confirmText={archiveMutation.isPending ? t('common.buttons.archiving') : t('common.buttons.archive')}
      destructive
      isLoading={archiveMutation.isPending}
      handleConfirm={handleConfirm}
    />
  );
}
