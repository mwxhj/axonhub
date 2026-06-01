'use client';

import { Archive, Info, KeyRound, Link, MoreHorizontal, Pencil, Power } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { PermissionGuard } from '@/components/permission-guard';
import { Button } from '@/components/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import { useCredentialsContext } from '../context/credentials-context';
import type { UpstreamCredential } from '../data/credentials';

interface CredentialsActionsProps {
  credential: UpstreamCredential;
}

export function CredentialsActions({ credential }: CredentialsActionsProps) {
  const { t } = useTranslation();
  const { setOpen, setCurrentCredential } = useCredentialsContext();

  const openDialog = (dialog: 'detail' | 'edit' | 'rotate' | 'status' | 'channels') => {
    setCurrentCredential(credential);
    setOpen(dialog);
  };

  return (
    <PermissionGuard requiredScope='write_channels'>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant='ghost' className='h-8 w-8 p-0'>
            <span className='sr-only'>{t('common.buttons.openMenu')}</span>
            <MoreHorizontal className='h-4 w-4' />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end'>
          <DropdownMenuItem onClick={() => openDialog('detail')}>
            <Info className='mr-2 h-4 w-4' />
            {t('credentials.actions.detail')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => openDialog('channels')}>
            <Link className='mr-2 h-4 w-4' />
            {t('credentials.actions.channels')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => openDialog('edit')}>
            <Pencil className='mr-2 h-4 w-4' />
            {t('common.buttons.edit')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => openDialog('rotate')}>
            <KeyRound className='mr-2 h-4 w-4' />
            {t('credentials.actions.rotate')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => openDialog('status')}>
            {credential.status === 'archived' ? <Power className='mr-2 h-4 w-4' /> : <Archive className='mr-2 h-4 w-4' />}
            {t('credentials.actions.status')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </PermissionGuard>
  );
}
