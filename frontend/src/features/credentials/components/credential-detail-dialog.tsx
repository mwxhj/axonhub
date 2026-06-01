'use client';

import { format } from 'date-fns';
import type { TFunction } from 'i18next';
import { AlertCircle, Archive, KeyRound, Link, Pencil } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useCredentialsContext } from '../context/credentials-context';
import { useUpstreamCredentialDetail } from '../data/credentials';
import type {
  CredentialExecution,
  CredentialRef,
  CredentialUsageLog,
  ProviderQuotaStatus,
  UpstreamCredentialDetail,
} from '../data/schema';

function shortIdentity(value?: string | null) {
  if (!value) {
    return '-';
  }
  if (value.length <= 32) {
    return value;
  }
  return `${value.slice(0, 15)}...${value.slice(-10)}`;
}

function dateLabel(value?: Date | string | null) {
  if (!value) {
    return '-';
  }
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '-';
  }
  return format(date, 'yyyy-MM-dd HH:mm');
}

function Field({ label, value, mono = false }: { label: string; value?: string | number | null; mono?: boolean }) {
  const displayValue = value === undefined || value === null || value === '' ? '-' : value;

  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className={`truncate text-sm ${mono ? 'font-mono' : ''}`}>{displayValue}</div>
    </div>
  );
}

function channelRefs(credential: UpstreamCredentialDetail): CredentialRef[] {
  return credential.channelRefs?.edges?.map((edge) => edge.node).filter((ref): ref is CredentialRef => Boolean(ref)) ?? [];
}

function executions(credential: UpstreamCredentialDetail): CredentialExecution[] {
  return credential.executions?.edges?.map((edge) => edge.node).filter((item): item is CredentialExecution => Boolean(item)) ?? [];
}

function usageLogs(credential: UpstreamCredentialDetail): CredentialUsageLog[] {
  return credential.usageLogs?.edges?.map((edge) => edge.node).filter((item): item is CredentialUsageLog => Boolean(item)) ?? [];
}

function providerQuotaStatuses(credential: UpstreamCredentialDetail): ProviderQuotaStatus[] {
  return credential.providerQuotaStatuses ?? [];
}

function providerQuotaStatusLabel(status: string | null | undefined, t: TFunction) {
  return status ? t(`credentials.providerQuota.status.${status}`, { defaultValue: status }) : t('credentials.providerQuota.status.unknown');
}

function routingAvailabilityKey(credential: UpstreamCredentialDetail, refs: CredentialRef[], providerStatuses: ProviderQuotaStatus[]) {
  if (credential.status === 'archived') {
    return 'archived';
  }
  if (credential.status === 'disabled') {
    return 'disabled';
  }
  if (refs.filter((ref) => ref.enabled).length === 0) {
    return 'noEnabledRefs';
  }
  if (providerStatuses.some((status) => !status.ready || status.status === 'exhausted')) {
    return 'blockedProviderQuota';
  }

  return 'selectable';
}

export function CredentialDetailDialog() {
  const { t } = useTranslation();
  const { open, setOpen, currentCredential, setCurrentCredential } = useCredentialsContext();
  const isOpen = open === 'detail' && !!currentCredential;
  const detailQuery = useUpstreamCredentialDetail(currentCredential?.id, { enabled: isOpen });
  const credential = (detailQuery.data ?? currentCredential) as UpstreamCredentialDetail | null;

  const close = () => {
    setOpen(null);
    setCurrentCredential(null);
  };

  const openRelatedDialog = (dialog: 'edit' | 'rotate' | 'channels' | 'archive') => {
    if (credential) {
      setCurrentCredential(credential);
    }
    setOpen(dialog);
  };

  const refs = credential ? channelRefs(credential) : [];
  const enabledRefs = refs.filter((ref) => ref.enabled);
  const providerStatuses = credential ? providerQuotaStatuses(credential) : [];
  const recentExecutions = credential ? executions(credential) : [];
  const recentUsageLogs = credential ? usageLogs(credential) : [];
  const routingKey = credential ? routingAvailabilityKey(credential, refs, providerStatuses) : 'selectable';

  return (
    <Dialog open={isOpen} onOpenChange={(nextOpen) => (nextOpen ? setOpen('detail') : close())}>
      <DialogContent className='sm:max-w-[980px]'>
        <DialogHeader>
          <DialogTitle>{credential?.name || t('credentials.unnamed')}</DialogTitle>
          <DialogDescription>{t('credentials.dialogs.detail.description')}</DialogDescription>
        </DialogHeader>

        {!credential || detailQuery.isLoading ? (
          <div className='text-muted-foreground py-8 text-center text-sm'>{t('common.loading')}</div>
        ) : (
          <div className='max-h-[72vh] overflow-y-auto pr-1'>
            <div className='mb-4 flex flex-wrap items-center gap-2'>
              <Badge variant={credential.status === 'enabled' ? 'default' : 'secondary'}>
                {t(`credentials.status.${credential.status}`)}
              </Badge>
              <Badge variant={routingKey === 'selectable' ? 'default' : 'secondary'}>
                {t(`credentials.routingAvailability.${routingKey}`)}
              </Badge>
              {credential.keyHint && <Badge variant='secondary'>{credential.keyHint}</Badge>}
            </div>

            {credential.lastError && (
              <Alert className='mb-4 border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900/60 dark:bg-amber-950/40 dark:text-amber-200'>
                <AlertCircle className='h-4 w-4' />
                <AlertDescription>{credential.lastError}</AlertDescription>
              </Alert>
            )}

            <Tabs defaultValue='overview' className='w-full'>
              <TabsList className='grid w-full grid-cols-4'>
                <TabsTrigger value='overview'>{t('credentials.detail.tabs.overview')}</TabsTrigger>
                <TabsTrigger value='channels'>{t('credentials.detail.tabs.channels')}</TabsTrigger>
                <TabsTrigger value='quota'>{t('credentials.detail.tabs.quota')}</TabsTrigger>
                <TabsTrigger value='history'>{t('credentials.detail.tabs.history')}</TabsTrigger>
              </TabsList>

              <TabsContent value='overview' className='mt-4 space-y-4'>
                <div className='grid gap-4 rounded-md border p-3 md:grid-cols-3'>
                  <Field label={t('credentials.fields.keyHint')} value={credential.keyHint} mono />
                  <Field
                    label={t('credentials.fields.secretFingerprint')}
                    value={shortIdentity(credential.secretFingerprint || credential.fingerprint)}
                    mono
                  />
                  <Field label={t('credentials.fields.status')} value={t(`credentials.status.${credential.status}`)} />
                  <Field label={t('credentials.columns.channels')} value={`${enabledRefs.length} / ${refs.length}`} />
                  <Field
                    label={t('credentials.columns.providerQuota')}
                    value={providerStatuses.length || t('credentials.providerQuota.empty')}
                  />
                  <Field label={t('credentials.detail.routingAvailability')} value={t(`credentials.routingAvailability.${routingKey}`)} />
                  <Field label={t('common.columns.updatedAt')} value={dateLabel(credential.updatedAt)} />
                </div>
                {credential.remark && (
                  <div className='rounded-md border p-3'>
                    <div className='text-muted-foreground text-xs'>{t('credentials.fields.remark')}</div>
                    <p className='mt-1 text-sm whitespace-pre-wrap'>{credential.remark}</p>
                  </div>
                )}
              </TabsContent>

              <TabsContent value='channels' className='mt-4 space-y-3'>
                {refs.length > 0 ? (
                  refs.map((ref) => (
                    <div key={ref.id} className='grid gap-3 rounded-md border p-3 md:grid-cols-[1fr_auto] md:items-center'>
                      <div className='min-w-0'>
                        <div className='truncate font-medium'>{ref.channel?.name || ref.channelID}</div>
                        <div className='text-muted-foreground mt-1 truncate text-xs'>
                          {[ref.channel?.type, ref.channel?.baseURL].filter(Boolean).join(' · ') || '-'}
                        </div>
                      </div>
                      <Badge variant={ref.enabled ? 'default' : 'secondary'}>
                        {ref.enabled ? t('credentials.fields.enabled') : t('credentials.fields.disabled')}
                      </Badge>
                    </div>
                  ))
                ) : (
                  <div className='text-muted-foreground rounded-md border py-8 text-center text-sm'>
                    {t('credentials.dialogs.channels.empty')}
                  </div>
                )}
              </TabsContent>

              <TabsContent value='quota' className='mt-4 space-y-4'>
                <section className='space-y-3 rounded-md border p-3'>
                  <h4 className='text-sm font-medium'>{t('credentials.providerQuota.title')}</h4>
                  {providerStatuses.length > 0 ? (
                    <div className='space-y-3'>
                      {providerStatuses.map((status) => (
                        <div key={status.id} className='grid gap-3 border-t pt-3 first:border-t-0 first:pt-0 md:grid-cols-3'>
                          <Field label={t('credentials.providerQuota.provider')} value={status.providerType} />
                          <Field label={t('credentials.fields.status')} value={providerQuotaStatusLabel(status.status, t)} />
                          <Field
                            label={t('credentials.providerQuota.ready')}
                            value={status.ready ? t('credentials.common.yes') : t('credentials.common.no')}
                          />
                          <Field label={t('credentials.providerQuota.nextResetAt')} value={dateLabel(status.nextResetAt)} />
                          <Field label={t('credentials.providerQuota.nextCheckAt')} value={dateLabel(status.nextCheckAt)} />
                          <Field label={t('credentials.providerQuota.scopeKey')} value={shortIdentity(status.scopeKey)} mono />
                          <Field
                            label={t('credentials.providerQuota.resourceScopeKey')}
                            value={shortIdentity(status.resourceScopeKey)}
                            mono
                          />
                          <Field label={t('common.columns.updatedAt')} value={dateLabel(status.updatedAt)} />
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className='text-muted-foreground text-sm'>{t('credentials.providerQuota.empty')}</div>
                  )}
                </section>

                <section className='space-y-3 rounded-md border p-3'>
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <h4 className='text-sm font-medium'>{t('credentials.detail.routingAvailability')}</h4>
                    <Badge variant={routingKey === 'selectable' ? 'default' : 'secondary'}>
                      {t(`credentials.routingAvailability.${routingKey}`)}
                    </Badge>
                  </div>
                  <div className='grid gap-4 md:grid-cols-3'>
                    <Field label={t('credentials.fields.status')} value={t(`credentials.status.${credential.status}`)} />
                    <Field label={t('credentials.detail.enabledRefs')} value={`${enabledRefs.length} / ${refs.length}`} />
                    <Field
                      label={t('credentials.detail.providerQuotaBlocksRouting')}
                      value={
                        providerStatuses.some((status) => !status.ready || status.status === 'exhausted')
                          ? t('credentials.common.yes')
                          : t('credentials.common.no')
                      }
                    />
                  </div>
                </section>
              </TabsContent>

              <TabsContent value='history' className='mt-4 grid gap-4 lg:grid-cols-2'>
                <div className='space-y-3'>
                  <h4 className='text-sm font-medium'>{t('credentials.detail.executions')}</h4>
                  {recentExecutions.length > 0 ? (
                    recentExecutions.map((execution) => (
                      <div key={execution.id} className='grid gap-2 rounded-md border p-3'>
                        <div className='flex min-w-0 items-center justify-between gap-3'>
                          <span className='truncate text-sm font-medium'>{execution.modelID}</span>
                          <Badge variant={execution.status === 'completed' ? 'default' : 'secondary'}>{execution.status}</Badge>
                        </div>
                        <div className='text-muted-foreground truncate text-xs'>
                          {[dateLabel(execution.createdAt), execution.channel?.name, execution.credentialSource]
                            .filter(Boolean)
                            .join(' · ')}
                        </div>
                        <div className='text-muted-foreground truncate font-mono text-xs'>
                          {execution.credentialKeyHint || execution.resourceScopeKey || '-'}
                        </div>
                        {execution.errorMessage && <div className='text-destructive truncate text-xs'>{execution.errorMessage}</div>}
                      </div>
                    ))
                  ) : (
                    <div className='text-muted-foreground rounded-md border py-8 text-center text-sm'>
                      {t('credentials.detail.emptyExecutions')}
                    </div>
                  )}
                </div>

                <div className='space-y-3'>
                  <h4 className='text-sm font-medium'>{t('credentials.detail.usage')}</h4>
                  {recentUsageLogs.length > 0 ? (
                    recentUsageLogs.map((usage) => (
                      <div key={usage.id} className='grid gap-2 rounded-md border p-3'>
                        <div className='flex min-w-0 items-center justify-between gap-3'>
                          <span className='truncate text-sm font-medium'>{usage.modelID}</span>
                          <span className='text-muted-foreground font-mono text-xs'>{usage.totalTokens.toLocaleString()}</span>
                        </div>
                        <div className='text-muted-foreground truncate text-xs'>
                          {[dateLabel(usage.createdAt), usage.channel?.name, usage.source].filter(Boolean).join(' · ')}
                        </div>
                        <div className='text-muted-foreground truncate text-xs'>
                          {t('credentials.detail.tokenBreakdown', {
                            prompt: usage.promptTokens.toLocaleString(),
                            completion: usage.completionTokens.toLocaleString(),
                          })}
                        </div>
                        {typeof usage.totalCost === 'number' && (
                          <div className='text-muted-foreground font-mono text-xs'>${usage.totalCost.toFixed(6)}</div>
                        )}
                      </div>
                    ))
                  ) : (
                    <div className='text-muted-foreground rounded-md border py-8 text-center text-sm'>
                      {t('credentials.detail.emptyUsage')}
                    </div>
                  )}
                </div>
              </TabsContent>
            </Tabs>
          </div>
        )}

        <DialogFooter className='gap-2 sm:justify-between'>
          <div className='flex flex-wrap gap-2'>
            <Button type='button' variant='outline' onClick={() => openRelatedDialog('channels')} disabled={!credential}>
              <Link className='mr-2 h-4 w-4' />
              {t('credentials.actions.channels')}
            </Button>
            <Button type='button' variant='outline' onClick={() => openRelatedDialog('edit')} disabled={!credential}>
              <Pencil className='mr-2 h-4 w-4' />
              {t('common.buttons.edit')}
            </Button>
            <Button type='button' variant='outline' onClick={() => openRelatedDialog('rotate')} disabled={!credential}>
              <KeyRound className='mr-2 h-4 w-4' />
              {t('credentials.actions.rotate')}
            </Button>
            {credential?.status !== 'archived' && (
              <Button type='button' variant='destructive' onClick={() => openRelatedDialog('archive')} disabled={!credential}>
                <Archive className='mr-2 h-4 w-4' />
                {t('credentials.actions.archive')}
              </Button>
            )}
          </div>
          <Button type='button' variant='outline' onClick={close}>
            {t('common.buttons.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
