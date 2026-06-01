import { format } from 'date-fns';
import { ColumnDef } from '@tanstack/react-table';
import { TFunction } from 'i18next';
import { Badge } from '@/components/ui/badge';
import type { UpstreamCredential } from '../data/credentials';
import { CredentialsActions } from './credentials-actions';

function shortFingerprint(value: string) {
  if (value.length <= 24) {
    return value;
  }
  return `${value.slice(0, 13)}...${value.slice(-8)}`;
}

function providerQuotaLabel(credential: UpstreamCredential, t: TFunction) {
  const statuses = credential.providerQuotaStatuses ?? [];
  if (statuses.length === 0) {
    return t('credentials.providerQuota.empty');
  }

  const exhausted = statuses.find((status) => !status.ready || status.status === 'exhausted');
  const status = exhausted ?? statuses.find((item) => item.status === 'warning') ?? statuses[0];

  return `${t(`credentials.providerQuota.status.${status.status}`, { defaultValue: status.status })} · ${status.providerType}`;
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
    id: 'providerQuota',
    header: t('credentials.columns.providerQuota'),
    cell: ({ row }) => <span className='text-muted-foreground line-clamp-2 text-xs'>{providerQuotaLabel(row.original, t)}</span>,
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
    cell: ({ row }) => (
      <span className='text-muted-foreground text-sm whitespace-nowrap'>
        {format(new Date(row.original.updatedAt), 'yyyy-MM-dd HH:mm')}
      </span>
    ),
  },
  {
    id: 'actions',
    header: t('common.columns.actions'),
    cell: ({ row }) => <CredentialsActions credential={row.original} />,
  },
];
