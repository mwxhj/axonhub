'use client';

import { useEffect, useMemo, useState } from 'react';
import { Loader2, Plus, Save, ShieldAlert, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { useSystemContext } from '../context/system-context';
import {
  type ResponseQualityGuardReasoningComparison,
  type ResponseQualityGuardRule,
  useResponseQualityGuardStats,
  useResponseQualityGuardSettings,
  useUpdateResponseQualityGuardSettings,
} from '../data/system';

const RESPONSE_QUALITY_GUARD_MODES = ['observe_only', 'retry_on_match'] as const;
const RESPONSE_QUALITY_GUARD_COMPARISONS = ['LTE', 'EQ'] as const satisfies readonly ResponseQualityGuardReasoningComparison[];

function normalizeRule(rule: ResponseQualityGuardRule): ResponseQualityGuardRule {
  return {
    modelMatch: rule.modelMatch.map((item) => item.trim()).filter(Boolean),
    reasoningTokensComparison: RESPONSE_QUALITY_GUARD_COMPARISONS.includes(rule.reasoningTokensComparison)
      ? rule.reasoningTokensComparison
      : 'LTE',
    reasoningTokensLTE: Math.max(0, Number.isFinite(rule.reasoningTokensLTE) ? Math.trunc(rule.reasoningTokensLTE) : 0),
    applyToStream: Boolean(rule.applyToStream),
    applyToNonStream: Boolean(rule.applyToNonStream),
    bufferStreamUntilDecision: Boolean(rule.bufferStreamUntilDecision),
  };
}

function normalizeRules(rules: ResponseQualityGuardRule[]): ResponseQualityGuardRule[] {
  return rules.map(normalizeRule);
}

function createEmptyRule(): ResponseQualityGuardRule {
  return {
    modelMatch: [],
    reasoningTokensComparison: 'LTE',
    reasoningTokensLTE: 516,
    applyToStream: true,
    applyToNonStream: true,
    bufferStreamUntilDecision: true,
  };
}

function sameRules(left: ResponseQualityGuardRule[], right: ResponseQualityGuardRule[]) {
  return JSON.stringify(normalizeRules(left)) === JSON.stringify(normalizeRules(right));
}

export function RetrySettings() {
  const { t } = useTranslation();
  const { data: settings, isLoading: isLoadingSettings } = useResponseQualityGuardSettings();
  const { data: stats, isLoading: isLoadingStats } = useResponseQualityGuardStats();
  const updateSettings = useUpdateResponseQualityGuardSettings();
  const { isLoading, setIsLoading } = useSystemContext();

  const [enabled, setEnabled] = useState(false);
  const [mode, setMode] = useState<(typeof RESPONSE_QUALITY_GUARD_MODES)[number]>('observe_only');
  const [rules, setRules] = useState<ResponseQualityGuardRule[]>([]);

  useEffect(() => {
    if (!settings) {
      return;
    }
    setEnabled(settings.enabled);
    setMode(settings.mode === 'retry_on_match' ? 'retry_on_match' : 'observe_only');
    setRules(settings.rules.length > 0 ? settings.rules.map((rule) => ({ ...rule, modelMatch: [...rule.modelMatch] })) : []);
  }, [settings]);

  const normalizedRules = useMemo(() => normalizeRules(rules), [rules]);

  const hasChanges = settings
    ? settings.enabled !== enabled || settings.mode !== mode || !sameRules(settings.rules, normalizedRules)
    : false;

  const updateRule = (index: number, next: Partial<ResponseQualityGuardRule>) => {
    setRules((current) =>
      current.map((rule, ruleIndex) => {
        if (ruleIndex !== index) {
          return rule;
        }
        return { ...rule, ...next };
      })
    );
  };

  const handleSave = async () => {
    setIsLoading(true);
    try {
      await updateSettings.mutateAsync({
        enabled,
        mode,
        rules: normalizedRules,
      });
    } finally {
      setIsLoading(false);
    }
  };

  if (isLoadingSettings) {
    return (
      <div className='flex h-32 items-center justify-center'>
        <Loader2 className='h-6 w-6 animate-spin' />
        <span className='text-muted-foreground ml-2'>{t('common.loading')}</span>
      </div>
    );
  }

  return (
    <div className='space-y-6'>
      <Card>
        <CardHeader>
          <CardTitle>{t('system.retry.title')}</CardTitle>
          <CardDescription>{t('system.retry.description')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-6'>
          <div className='grid gap-4 md:grid-cols-2'>
            <div className='rounded-lg border p-4'>
              <div className='text-muted-foreground text-sm'>{t('system.retry.responseQualityGuard.stats.matchedCount.label')}</div>
              <div className='mt-2 text-3xl font-semibold'>
                {isLoadingStats ? <Loader2 className='h-6 w-6 animate-spin' /> : stats?.matchedCount ?? 0}
              </div>
              <div className='text-muted-foreground mt-2 text-sm'>
                {t('system.retry.responseQualityGuard.stats.matchedCount.description')}
              </div>
            </div>
          </div>

          <div className='flex items-center justify-between gap-4'>
            <div className='space-y-0.5'>
              <Label htmlFor='response-quality-guard-enabled'>{t('system.retry.responseQualityGuard.enabled.label')}</Label>
              <div className='text-muted-foreground text-sm'>{t('system.retry.responseQualityGuard.enabled.description')}</div>
            </div>
            <Switch
              id='response-quality-guard-enabled'
              checked={enabled}
              onCheckedChange={setEnabled}
              disabled={updateSettings.isPending}
            />
          </div>

          <div className='grid gap-2 max-w-sm'>
            <Label htmlFor='response-quality-guard-mode'>{t('system.retry.responseQualityGuard.mode.label')}</Label>
            <Select value={mode} onValueChange={(value) => setMode(value as (typeof RESPONSE_QUALITY_GUARD_MODES)[number])}>
              <SelectTrigger id='response-quality-guard-mode' disabled={updateSettings.isPending}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value='observe_only'>{t('system.retry.responseQualityGuard.mode.options.observe_only')}</SelectItem>
                <SelectItem value='retry_on_match'>{t('system.retry.responseQualityGuard.mode.options.retry_on_match')}</SelectItem>
              </SelectContent>
            </Select>
            <div className='text-muted-foreground text-sm'>{t('system.retry.responseQualityGuard.mode.description')}</div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{t('system.retry.responseQualityGuard.rules.title')}</CardTitle>
          <CardDescription>{t('system.retry.responseQualityGuard.rules.description')}</CardDescription>
        </CardHeader>
        <CardContent className='space-y-4'>
          {rules.length === 0 ? (
            <div className='text-muted-foreground rounded-md border border-dashed p-4 text-sm'>
              {t('system.retry.responseQualityGuard.rules.empty')}
            </div>
          ) : (
            rules.map((rule, index) => (
              <div key={index} className='space-y-4 rounded-lg border p-4'>
                <div className='flex items-start justify-between gap-4'>
                  <div className='flex items-center gap-2'>
                    <ShieldAlert className='text-muted-foreground h-4 w-4' />
                    <div className='font-medium'>{t('system.retry.responseQualityGuard.rules.ruleTitle', { index: index + 1 })}</div>
                  </div>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    onClick={() => setRules((current) => current.filter((_, ruleIndex) => ruleIndex !== index))}
                    disabled={updateSettings.isPending}
                    aria-label={t('system.retry.responseQualityGuard.rules.remove')}
                  >
                    <Trash2 className='h-4 w-4' />
                  </Button>
                </div>

                <div className='grid gap-2'>
                  <Label htmlFor={`rqg-model-match-${index}`}>{t('system.retry.responseQualityGuard.rules.modelMatch.label')}</Label>
                  <Textarea
                    id={`rqg-model-match-${index}`}
                    value={rule.modelMatch.join('\n')}
                    onChange={(event) =>
                      updateRule(index, {
                        modelMatch: event.target.value
                          .split('\n')
                          .map((item) => item.trim())
                          .filter(Boolean),
                      })
                    }
                    placeholder={t('system.retry.responseQualityGuard.rules.modelMatch.placeholder')}
                    rows={4}
                    disabled={updateSettings.isPending}
                  />
                  <div className='text-muted-foreground text-sm'>{t('system.retry.responseQualityGuard.rules.modelMatch.description')}</div>
                </div>

                <div className='grid gap-4 md:grid-cols-2'>
                  <div className='grid gap-2'>
                    <Label>{t('system.retry.responseQualityGuard.rules.reasoningTokens.label')}</Label>
                    <div className='grid gap-3 sm:grid-cols-[140px_minmax(0,1fr)]'>
                      <Select
                        value={rule.reasoningTokensComparison}
                        onValueChange={(value) =>
                          updateRule(index, {
                            reasoningTokensComparison: value as ResponseQualityGuardReasoningComparison,
                          })
                        }
                      >
                        <SelectTrigger disabled={updateSettings.isPending}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value='LTE'>{t('system.retry.responseQualityGuard.rules.reasoningTokensComparison.options.lte')}</SelectItem>
                          <SelectItem value='EQ'>{t('system.retry.responseQualityGuard.rules.reasoningTokensComparison.options.eq')}</SelectItem>
                        </SelectContent>
                      </Select>
                      <Input
                        id={`rqg-threshold-${index}`}
                        type='number'
                        min={0}
                        step={1}
                        value={rule.reasoningTokensLTE}
                        onChange={(event) =>
                          updateRule(index, {
                            reasoningTokensLTE: event.target.value === '' ? 0 : Number.parseInt(event.target.value, 10) || 0,
                          })
                        }
                        disabled={updateSettings.isPending}
                      />
                    </div>
                    <div className='text-muted-foreground text-sm'>
                      {t('system.retry.responseQualityGuard.rules.reasoningTokens.description')}
                    </div>
                  </div>

                  <div className='grid gap-3'>
                    <div className='text-sm font-medium'>{t('system.retry.responseQualityGuard.rules.appliesTo.label')}</div>
                    <label className='flex items-center gap-3 text-sm'>
                      <Checkbox
                        checked={rule.applyToStream}
                        onCheckedChange={(checked) => updateRule(index, { applyToStream: checked === true })}
                        disabled={updateSettings.isPending}
                      />
                      <span>{t('system.retry.responseQualityGuard.rules.appliesTo.stream')}</span>
                    </label>
                    <label className='flex items-center gap-3 text-sm'>
                      <Checkbox
                        checked={rule.applyToNonStream}
                        onCheckedChange={(checked) => updateRule(index, { applyToNonStream: checked === true })}
                        disabled={updateSettings.isPending}
                      />
                      <span>{t('system.retry.responseQualityGuard.rules.appliesTo.nonStream')}</span>
                    </label>
                    <label className='flex items-center gap-3 text-sm'>
                      <Checkbox
                        checked={rule.bufferStreamUntilDecision}
                        onCheckedChange={(checked) => updateRule(index, { bufferStreamUntilDecision: checked === true })}
                        disabled={updateSettings.isPending}
                      />
                      <span>{t('system.retry.responseQualityGuard.rules.bufferStreamUntilDecision.label')}</span>
                    </label>
                    <div className='text-muted-foreground text-sm'>
                      {t('system.retry.responseQualityGuard.rules.bufferStreamUntilDecision.description')}
                    </div>
                  </div>
                </div>
              </div>
            ))
          )}

          <Button
            type='button'
            variant='outline'
            onClick={() => setRules((current) => [...current, createEmptyRule()])}
            disabled={updateSettings.isPending}
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('system.retry.responseQualityGuard.rules.add')}
          </Button>
        </CardContent>
      </Card>

      {hasChanges && (
        <div className='flex justify-end'>
          <Button onClick={handleSave} disabled={isLoading || updateSettings.isPending} className='min-w-[100px]'>
            {isLoading || updateSettings.isPending ? (
              <>
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                {t('system.buttons.saving')}
              </>
            ) : (
              <>
                <Save className='mr-2 h-4 w-4' />
                {t('system.buttons.save')}
              </>
            )}
          </Button>
        </div>
      )}
    </div>
  );
}
