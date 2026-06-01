import { ColumnDef } from '@tanstack/react-table';
import { TFunction } from 'i18next';
import { format } from 'date-fns';
import { Badge } from '@/components/ui/badge';
import type { UpstreamCredential } from '../data/credentials';
import { CredentialsActions } from './credentials-actions';

function shortFingerprint(value: string) {
  if (value.length <= 24) {
    return value;
  }
  return `${value.slice(0, 13)}...${value.slice(-8)}`;
}

function quotaLabel(credential: UpstreamCredential, t: TFunction) {
  const quota = credential.quotaScope;
  if (!quota) {
    return credential.quotaStatus || t('credentials.quota.status.unknown');
  }

  const pieces = [quota.usedAmount, quota.limitAmount].filter(Boolean);
  if (pieces.length === 2) {
    return `${pieces[0]} / ${pieces[1]} ${quota.unit ? t(`credentials.quota.units.${quota.unit}`) : ''}`;
  }

  return quota.status ? t(`credentials.quota.status.${quota.status}`) : t('credentials.quota.status.unknown');
}

export const createCredentialColumns = (t: TFunction): ColumnDef<UpstreamCredential>[] => [
  {
    accessorKey: 'name',
    header: t('common.columns.name'),
    cell: ({ row }) => {
      const name = row.original.name?.trim();
      const fingerprint = row.original.secretFingerprint || row.original.fingerprint || '';
      return (
        <div className='min-w-0'>
          <div className='truncate font-medium'>{name || t('credentials.unnamed')}</div>
          <div className='text-muted-foreground mt-1 truncate font-mono text-xs'>{fingerprint ? shortFingerprint(fingerprint) : '-'}</div>
        </div>
      );
    },
  },
  {
    accessorKey: 'keyHint',
    header: t('credentials.columns.keyHint'),
    cell: ({ row }) => <span className='text-muted-foreground font-mono text-xs'>{row.original.keyHint || '-'}</span>,
  },
  {
    id: 'quota',
    header: t('credentials.columns.quota'),
    cell: ({ row }) => {
      const quota = row.original.quotaScope;
      return (
        <div className='min-w-0'>
          <div className='truncate text-sm'>{quota?.name || t('credentials.quota.defaultScope')}</div>
          <div className='text-muted-foreground mt-1 truncate text-xs'>{quotaLabel(row.original, t)}</div>
        </div>
      );
    },
  },
  {
    accessorKey: 'lastError',
    header: t('credentials.columns.latestError'),
    cell: ({ row }) => <span className='text-muted-foreground line-clamp-2 text-xs'>{row.original.lastError || '-'}</span>,
  },
  {
    accessorKey: 'status',
    header: t('common.columns.status'),
    cell: ({ row }) => {
      const status = row.original.status;
      return <Badge variant={status === 'enabled' ? 'default' : 'secondary'}>{t(`credentials.status.${status}`)}</Badge>;
    },
  },
  {
    id: 'channels',
    header: t('credentials.columns.channels'),
    cell: ({ row }) => {
      const count = row.original.channelRefs?.totalCount ?? 0;
      return <span className='font-mono text-sm'>{count}</span>;
    },
  },
  {
    accessorKey: 'updatedAt',
    header: t('common.columns.updatedAt'),
    cell: ({ row }) => <span className='text-muted-foreground whitespace-nowrap text-sm'>{format(new Date(row.original.updatedAt), 'yyyy-MM-dd HH:mm')}</span>,
  },
  {
    id: 'actions',
    header: t('common.columns.actions'),
    cell: ({ row }) => <CredentialsActions credential={row.original} />,
  },
];
