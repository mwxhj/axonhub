import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useDebounce } from '@/hooks/use-debounce';
import { usePaginationSearch } from '@/hooks/use-pagination-search';
import { Header } from '@/components/layout/header';
import { Main } from '@/components/layout/main';
import { CredentialDialogs } from './components/credential-dialogs';
import { createCredentialColumns } from './components/credentials-columns';
import { CredentialsPrimaryButtons } from './components/credentials-primary-buttons';
import { CredentialsTable } from './components/credentials-table';
import CredentialsProvider from './context/credentials-context';
import { useUpstreamCredentials } from './data/credentials';
import type { CredentialStatus, UpstreamCredential } from './data/schema';

function isCredential(value: UpstreamCredential | null | undefined): value is UpstreamCredential {
  return Boolean(value);
}

function CredentialsContent() {
  const { t } = useTranslation();
  const { pageSize, setCursors, setPageSize, resetCursor, paginationArgs } = usePaginationSearch({
    defaultPageSize: 20,
    pageSizeStorageKey: 'credentials-table-page-size',
  });
  const [nameFilter, setNameFilter] = useState('');
  const [issuerFilter, setIssuerFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState<CredentialStatus | 'active'>('active');

  const debouncedNameFilter = useDebounce(nameFilter, 300);
  const debouncedIssuerFilter = useDebounce(issuerFilter, 300);

  const whereClause = useMemo(() => {
    const where: Record<string, unknown> = {};
    if (debouncedNameFilter) {
      where.or = [{ nameContainsFold: debouncedNameFilter }, { fingerprintContainsFold: debouncedNameFilter }];
    }
    if (debouncedIssuerFilter) {
      where.issuerScopeContainsFold = debouncedIssuerFilter;
    }
    if (statusFilter === 'active') {
      where.statusIn = ['enabled', 'disabled'];
    } else {
      where.status = statusFilter;
    }
    return where;
  }, [debouncedNameFilter, debouncedIssuerFilter, statusFilter]);

  const { data, isLoading } = useUpstreamCredentials({
    ...paginationArgs,
    where: whereClause,
    orderBy: {
      field: 'UPDATED_AT',
      direction: 'DESC',
    },
  });

  const handleNextPage = () => {
    if (data?.pageInfo?.hasNextPage && data?.pageInfo?.endCursor) {
      setCursors(data.pageInfo.startCursor ?? undefined, data.pageInfo.endCursor ?? undefined, 'after');
    }
  };

  const handlePreviousPage = () => {
    if (data?.pageInfo?.hasPreviousPage) {
      setCursors(data.pageInfo.startCursor ?? undefined, data.pageInfo.endCursor ?? undefined, 'before');
    }
  };

  const columns = createCredentialColumns(t);

  return (
    <div className='flex flex-1 flex-col overflow-hidden'>
      <CredentialsTable
        data={data?.edges?.map((edge) => edge.node).filter(isCredential) ?? []}
        columns={columns}
        loading={isLoading}
        pageInfo={data?.pageInfo}
        pageSize={pageSize}
        totalCount={data?.totalCount}
        nameFilter={nameFilter}
        issuerFilter={issuerFilter}
        statusFilter={statusFilter}
        onNextPage={handleNextPage}
        onPreviousPage={handlePreviousPage}
        onPageSizeChange={setPageSize}
        onNameFilterChange={(filter) => {
          setNameFilter(filter);
          resetCursor();
        }}
        onIssuerFilterChange={(filter) => {
          setIssuerFilter(filter);
          resetCursor();
        }}
        onStatusFilterChange={(filter) => {
          setStatusFilter(filter);
          resetCursor();
        }}
      />
    </div>
  );
}

export default function CredentialsManagement() {
  const { t } = useTranslation();

  return (
    <CredentialsProvider>
      <Header fixed>
        <div className='flex flex-1 items-center justify-between gap-4'>
          <div>
            <h2 className='text-xl font-bold tracking-tight'>{t('credentials.title')}</h2>
            <p className='text-sm text-muted-foreground'>{t('credentials.description')}</p>
          </div>
          <CredentialsPrimaryButtons />
        </div>
      </Header>

      <Main fixed>
        <CredentialsContent />
      </Main>
      <CredentialDialogs />
    </CredentialsProvider>
  );
}
