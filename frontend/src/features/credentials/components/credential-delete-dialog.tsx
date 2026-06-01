'use client';

import { Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import { ConfirmDialog } from '@/components/confirm-dialog';
import { useCredentialsContext } from '../context/credentials-context';
import { useDeleteUpstreamCredential } from '../data/credentials';

export function CredentialDeleteDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const deleteMutation = useDeleteUpstreamCredential();
  const isOpen = open === 'delete' && !!currentCredential;

  const close = () => {
    setOpen(null);
    setCurrentCredential(null);
  };

  const handleConfirm = async () => {
    if (!currentCredential) {
      return;
    }

    await deleteMutation.mutateAsync(currentCredential.id);
    close();
  };

  return (
    <ConfirmDialog
      open={isOpen}
      onOpenChange={(nextOpen) => (nextOpen ? setOpen('delete') : close())}
      title={
        <span className='flex items-center gap-2'>
          <Trash2 className='h-4 w-4' />
          {t('credentials.dialogs.delete.title')}
        </span>
      }
      desc={
        <div className='space-y-3'>
          <div>
            {t('credentials.dialogs.delete.description', {
              name: currentCredential?.name || currentCredential?.keyHint || currentCredential?.id,
            })}
          </div>
          {currentCredential?.keyHint && (
            <Badge variant='secondary' className='font-mono'>
              {currentCredential.keyHint}
            </Badge>
          )}
          <ul className='text-muted-foreground list-disc space-y-1 pl-5 text-sm'>
            <li>{t('credentials.dialogs.delete.effects.refs')}</li>
            <li>{t('credentials.dialogs.delete.effects.history')}</li>
            <li>{t('credentials.dialogs.delete.effects.recreate')}</li>
          </ul>
        </div>
      }
      cancelBtnText={t('common.buttons.cancel')}
      confirmText={t('common.buttons.delete')}
      destructive
      isLoading={deleteMutation.isPending}
      handleConfirm={handleConfirm}
    />
  );
}
