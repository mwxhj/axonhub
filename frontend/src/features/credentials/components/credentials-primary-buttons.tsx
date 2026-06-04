'use client';

import { Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { PermissionGuard } from '@/components/permission-guard';
import { Button } from '@/components/ui/button';
import { useCredentialsContext } from '../context/credentials-context';

export function CredentialsPrimaryButtons() {
  const { t } = useTranslation();
  const { setOpen } = useCredentialsContext();

  return (
    <PermissionGuard requiredScope='write_channels'>
      <div className='flex flex-wrap items-center gap-2'>
        <Button onClick={() => setOpen('create')}>
          <Plus className='mr-2 h-4 w-4' />
          {t('credentials.buttons.create')}
        </Button>
      </div>
    </PermissionGuard>
  );
}
