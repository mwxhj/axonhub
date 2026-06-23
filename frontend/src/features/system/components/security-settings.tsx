'use client';

import { useEffect, useMemo, useState } from 'react';
import { Loader2, Save, ShieldBan } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { TagsInput } from '@/components/ui/tags-input';
import { useSystemContext } from '../context/system-context';
import { useSecuritySettings, useUpdateSecuritySettings } from '../data/system';

function normalizeBlockedIPs(values: string[]) {
  const seen = new Set<string>();
  const normalized: string[] = [];
  for (const value of values) {
    const trimmed = value.trim();
    if (!trimmed || seen.has(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    normalized.push(trimmed);
  }
  return normalized;
}

function sameStringList(left: string[], right: string[]) {
  if (left.length !== right.length) {
    return false;
  }
  return left.every((value, index) => value === right[index]);
}

export function SecuritySettings() {
  const { t } = useTranslation();
  const { data: settings, isLoading: isLoadingSettings } = useSecuritySettings();
  const updateSettings = useUpdateSecuritySettings();
  const { isLoading, setIsLoading } = useSystemContext();
  const [blockedIPs, setBlockedIPs] = useState<string[]>([]);
  const [showRequestLogIPBanIcon, setShowRequestLogIPBanIcon] = useState(true);

  useEffect(() => {
    if (!settings) {
      return;
    }
    setBlockedIPs(normalizeBlockedIPs(settings.blockedIPs || []));
    setShowRequestLogIPBanIcon(settings.showRequestLogIPBanIcon);
  }, [settings]);

  const normalizedBlockedIPs = useMemo(() => normalizeBlockedIPs(blockedIPs), [blockedIPs]);

  const hasChanges = settings
    ? !sameStringList(normalizeBlockedIPs(settings.blockedIPs || []), normalizedBlockedIPs) ||
      settings.showRequestLogIPBanIcon !== showRequestLogIPBanIcon
    : false;

  const handleSave = async () => {
    setIsLoading(true);
    try {
      await updateSettings.mutateAsync({
        blockedIPs: normalizedBlockedIPs,
        showRequestLogIPBanIcon,
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
    <div className='space-y-8'>
      <div className='space-y-2'>
        <div className='flex items-center gap-2'>
          <ShieldBan className='text-muted-foreground h-5 w-5' />
          <h3 className='text-lg font-semibold'>{t('system.security.title')}</h3>
        </div>
        <p className='text-muted-foreground text-sm'>{t('system.security.description')}</p>
      </div>

      <div className='space-y-3'>
        <Label htmlFor='blocked-ips'>{t('system.security.blockedIPs.label')}</Label>
        <TagsInput
          id='blocked-ips'
          value={blockedIPs}
          onChange={setBlockedIPs}
          placeholder={t('system.security.blockedIPs.placeholder')}
          autoCapitalize='off'
          autoCorrect='off'
          spellCheck={false}
        />
        <div className='text-muted-foreground text-sm'>{t('system.security.blockedIPs.description')}</div>
      </div>

      <div className='flex items-center justify-between gap-4 border-t pt-6'>
        <div className='space-y-0.5'>
          <Label htmlFor='request-log-ip-ban-icon'>{t('system.security.requestLogIPBanIcon.label')}</Label>
          <div className='text-muted-foreground text-sm'>{t('system.security.requestLogIPBanIcon.description')}</div>
        </div>
        <Switch
          id='request-log-ip-ban-icon'
          checked={showRequestLogIPBanIcon}
          onCheckedChange={setShowRequestLogIPBanIcon}
          disabled={updateSettings.isPending}
        />
      </div>

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
