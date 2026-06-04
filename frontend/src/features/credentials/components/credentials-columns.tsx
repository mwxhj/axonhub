import { format } from 'date-fns';
import { ColumnDef } from '@tanstack/react-table';
import { TFunction } from 'i18next';
import { Badge } from '@/components/ui/badge';
import type { UpstreamCredential } from '../data/credentials';
import { CredentialsActions } from './credentials-actions';

function hasQuotaAmount(value: string | null | undefined) {
  return Boolean(value?.trim());
}

function formatQuotaAmount(value: string | null | undefined, fallback = '-') {
  const trimmed = value?.trim();
  if (!trimmed) {
    return fallback;
  }

  const numeric = Number(trimmed);
  if (!Number.isFinite(numeric)) {
    return trimmed;
  }

  return new Intl.NumberFormat(undefined, {
    maximumFractionDigits: 6,
  }).format(numeric);
}

function LocalQuotaCell({ credential, t }: { credential: UpstreamCredential; t: TFunction }) {
  const scope = credential.quotaScope;
  if (!scope) {
    return <span className='text-muted-foreground text-xs'>{t('credentials.quota.none')}</span>;
  }

  const shouldShowAmounts = hasQuotaAmount(scope.usedAmount) || hasQuotaAmount(scope.limitAmount);
  const usedAmount = formatQuotaAmount(scope.usedAmount, '0');
  const limitAmount = formatQuotaAmount(scope.limitAmount);
  const unit = scope.unit ? t(`credentials.quota.units.${scope.unit}`, { defaultValue: scope.unit }) : '';
  const amountLabel = `${usedAmount} / ${limitAmount}${unit ? ` ${unit}` : ''} · ${t('credentials.quota.todayUsage')}`;

  return (
    <div className='min-w-40 space-y-0.5 text-xs'>
      {shouldShowAmounts ? (
        <div className='truncate' title={amountLabel}>
          <span className='font-mono'>{usedAmount}</span>
          <span className='text-muted-foreground'> / </span>
          <span className='font-mono'>{limitAmount}</span>
          {unit ? <span className='text-muted-foreground'> {unit}</span> : null}
          <span className='text-muted-foreground'> · {t('credentials.quota.todayUsage')}</span>
        </div>
      ) : (
        <div className='text-muted-foreground truncate'>{t('credentials.quota.configured')}</div>
      )}
    </div>
  );
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
        </div>
      );
    },
  },
  {
    id: 'localQuota',
    header: t('credentials.columns.localQuota'),
    cell: ({ row }) => <LocalQuotaCell credential={row.original} t={t} />,
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
