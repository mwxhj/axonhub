'use client';

import { Plus, RefreshCw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { PermissionGuard } from '@/components/permission-guard';
import { Button } from '@/components/ui/button';
import { useCredentialsContext } from '../context/credentials-context';
import { useMigrateLegacyChannelCredentials } from '../data/credentials';

export function CredentialsPrimaryButtons() {
  const { t } = useTranslation();
  const { setOpen } = useCredentialsContext();
  const migrate = useMigrateLegacyChannelCredentials();

  return (
    <PermissionGuard requiredScope='write_channels'>
      <div className='flex flex-wrap items-center gap-2'>
        <Button variant='outline' onClick={() => migrate.mutate()} disabled={migrate.isPending}>
          <RefreshCw className='mr-2 h-4 w-4' />
          {migrate.isPending ? t('common.buttons.processing') : t('credentials.buttons.migrateLegacy')}
        </Button>
        <Button onClick={() => setOpen('create')}>
          <Plus className='mr-2 h-4 w-4' />
          {t('credentials.buttons.create')}
        </Button>
      </div>
    </PermissionGuard>
  );
}
