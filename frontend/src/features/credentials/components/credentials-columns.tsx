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

export const createCredentialColumns = (t: TFunction): ColumnDef<UpstreamCredential>[] => [
  {
    accessorKey: 'name',
    header: t('common.columns.name'),
    cell: ({ row }) => {
      const name = row.original.name?.trim();
      return (
        <div className='min-w-0'>
          <div className='truncate font-medium'>{name || t('credentials.unnamed')}</div>
          <div className='text-muted-foreground mt-1 truncate font-mono text-xs'>{shortFingerprint(row.original.fingerprint)}</div>
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
    accessorKey: 'secretKind',
    header: t('credentials.columns.secretKind'),
    cell: ({ row }) => <span className='whitespace-nowrap'>{t(`credentials.authKinds.${row.original.secretKind}`)}</span>,
  },
  {
    accessorKey: 'issuerScope',
    header: t('credentials.columns.issuerScope'),
    cell: ({ row }) => <Badge variant='outline'>{row.original.issuerScope || '-'}</Badge>,
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
    accessorKey: 'weight',
    header: t('credentials.columns.weight'),
    cell: ({ row }) => <span className='font-mono text-sm'>{row.original.weight}</span>,
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
