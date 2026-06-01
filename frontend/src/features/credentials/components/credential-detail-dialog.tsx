'use client';

import { format } from 'date-fns';
import type { TFunction } from 'i18next';
import { AlertCircle, KeyRound, Link, Pencil } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { useCredentialsContext } from '../context/credentials-context';
import { useUpstreamCredentialDetail } from '../data/credentials';
import type { CredentialExecution, CredentialRef, CredentialUsageLog, UpstreamCredentialDetail } from '../data/schema';

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
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className={`truncate text-sm ${mono ? 'font-mono' : ''}`}>{value || '-'}</div>
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

function quotaStatusLabel(status: string | null | undefined, t: TFunction) {
  return status ? t(`credentials.quota.status.${status}`, { defaultValue: status }) : t('credentials.quota.status.unknown');
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

  const openRelatedDialog = (dialog: 'edit' | 'rotate' | 'channels') => {
    if (credential) {
      setCurrentCredential(credential);
    }
    setOpen(dialog);
  };

  const quota = credential?.quotaScope;
  const refs = credential ? channelRefs(credential) : [];
  const recentExecutions = credential ? executions(credential) : [];
  const recentUsageLogs = credential ? usageLogs(credential) : [];
  const quotaStatus = quota?.status || credential?.quotaStatus || 'unknown';

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
              <Badge variant={credential.status === 'enabled' ? 'default' : 'secondary'}>{t(`credentials.status.${credential.status}`)}</Badge>
              <Badge variant='outline'>{quotaStatusLabel(quotaStatus, t)}</Badge>
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
                  <Field label={t('credentials.fields.secretFingerprint')} value={shortIdentity(credential.secretFingerprint || credential.fingerprint)} mono />
                  <Field label={t('credentials.fields.status')} value={t(`credentials.status.${credential.status}`)} />
                  <Field label={t('credentials.columns.channels')} value={refs.length} />
                  <Field label={t('credentials.columns.quota')} value={quota?.name || quotaStatusLabel(quotaStatus, t)} />
                  <Field label={t('common.columns.updatedAt')} value={dateLabel(credential.updatedAt)} />
                </div>
                {credential.remark && (
                  <div className='rounded-md border p-3'>
                    <div className='text-muted-foreground text-xs'>{t('credentials.fields.remark')}</div>
                    <p className='mt-1 whitespace-pre-wrap text-sm'>{credential.remark}</p>
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
                  <div className='text-muted-foreground rounded-md border py-8 text-center text-sm'>{t('credentials.dialogs.channels.empty')}</div>
                )}
              </TabsContent>

              <TabsContent value='quota' className='mt-4 space-y-4'>
                <div className='grid gap-4 rounded-md border p-3 md:grid-cols-3'>
                  <Field label={t('credentials.fields.quotaScopeName')} value={quota?.name || t('credentials.quota.defaultScope')} />
                  <Field label={t('credentials.fields.quotaUnit')} value={quota?.unit ? t(`credentials.quota.units.${quota.unit}`) : '-'} />
                  <Field label={t('credentials.fields.status')} value={quotaStatusLabel(quotaStatus, t)} />
                  <Field label={t('credentials.fields.quotaLimitAmount')} value={quota?.limitAmount} mono />
                  <Field label={t('credentials.fields.quotaUsedAmount')} value={quota?.usedAmount} mono />
                  <Field label={t('credentials.fields.quotaWarningThresholdPercent')} value={quota?.warningThresholdPercent} />
                  <Field label={t('credentials.fields.quotaResetPolicy')} value={quota?.resetPolicy ? t(`credentials.quota.resetPolicies.${quota.resetPolicy}`) : '-'} />
                  <Field label={t('credentials.fields.quotaResetAt')} value={dateLabel(quota?.resetAt)} />
                  <Field label={t('credentials.fields.quotaOverLimitAction')} value={quota?.overLimitAction ? t(`credentials.quota.overLimitActions.${quota.overLimitAction}`) : '-'} />
                </div>
                {(quota?.lastError || quota?.remark) && (
                  <div className='grid gap-3 rounded-md border p-3'>
                    {quota?.lastError && <Field label={t('credentials.detail.latestError')} value={quota.lastError} />}
                    {quota?.remark && <Field label={t('credentials.fields.quotaRemark')} value={quota.remark} />}
                  </div>
                )}
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
                          {[dateLabel(execution.createdAt), execution.channel?.name, execution.credentialSource].filter(Boolean).join(' · ')}
                        </div>
                        <div className='text-muted-foreground truncate font-mono text-xs'>
                          {execution.credentialKeyHint || execution.resourceScopeKey || '-'}
                        </div>
                        {execution.errorMessage && <div className='text-destructive truncate text-xs'>{execution.errorMessage}</div>}
                      </div>
                    ))
                  ) : (
                    <div className='text-muted-foreground rounded-md border py-8 text-center text-sm'>{t('credentials.detail.emptyExecutions')}</div>
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
                    <div className='text-muted-foreground rounded-md border py-8 text-center text-sm'>{t('credentials.detail.emptyUsage')}</div>
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
          </div>
          <Button type='button' variant='outline' onClick={close}>
            {t('common.buttons.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
