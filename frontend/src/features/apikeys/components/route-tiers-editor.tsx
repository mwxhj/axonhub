import { useMemo, useState } from 'react';
import type { TFunction } from 'i18next';
import type { FieldValues, Path, PathValue, UseFormReturn } from 'react-hook-form';
import { IconArrowDown, IconArrowUp, IconPlus, IconTrash } from '@tabler/icons-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { extractNumberID } from '@/lib/utils';
import { useAllChannelSummarys } from '@/features/channels/data/channels';

type RouteTierValue = {
  name?: string;
  channelIDs?: number[];
};

type ChannelOption = {
  id: number;
  name: string;
  type: string;
};

type RouteTiersEditorProps<TFieldValues extends FieldValues> = {
  form: UseFormReturn<TFieldValues>;
  routeTiersName: string;
  preferredChannelName: string;
  selectedProjectId?: string | null;
  t: TFunction;
};

const formSetOptions = { shouldDirty: true, shouldValidate: true } as const;

export function RouteTiersEditor<TFieldValues extends FieldValues>({
  form,
  routeTiersName,
  preferredChannelName,
  selectedProjectId,
  t,
}: RouteTiersEditorProps<TFieldValues>) {
  const [selectResetKey, setSelectResetKey] = useState(0);
  const { data: channelsData } = useAllChannelSummarys(selectedProjectId, { enabled: true });
  const routeTiersPath = routeTiersName as Path<TFieldValues>;
  const preferredChannelPath = preferredChannelName as Path<TFieldValues>;

  const channelOptions = useMemo<ChannelOption[]>(() => {
    return (
      channelsData?.edges
        ?.map((edge) => ({
          id: Number.parseInt(extractNumberID(edge.node.id), 10),
          name: edge.node.name,
          type: edge.node.type,
        }))
        .filter((channel) => Number.isFinite(channel.id)) ?? []
    );
  }, [channelsData]);

  const channelByID = useMemo(() => new Map(channelOptions.map((channel) => [channel.id, channel])), [channelOptions]);
  const tiers = ((form.watch(routeTiersPath) as RouteTierValue[] | null | undefined) ?? []) as RouteTierValue[];
  const preferredChannelID = form.watch(preferredChannelPath) as number | null | undefined;

  const routeChannelIDs = useMemo(() => {
    const ids: number[] = [];
    const seen = new Set<number>();
    tiers.forEach((tier) => {
      (tier.channelIDs ?? []).forEach((id) => {
        if (id > 0 && !seen.has(id)) {
          seen.add(id);
          ids.push(id);
        }
      });
    });
    return ids;
  }, [tiers]);

  const cleanupPreferredChannel = (nextTiers: RouteTierValue[]) => {
    const nextIDs = new Set(nextTiers.flatMap((tier) => tier.channelIDs ?? []));
    const preferred = form.getValues(preferredChannelPath) as number | null | undefined;
    if (preferred != null && !nextIDs.has(preferred)) {
      form.setValue(preferredChannelPath, null as PathValue<TFieldValues, Path<TFieldValues>>, formSetOptions);
    }
  };

  const setTiers = (next: RouteTierValue[]) => {
    form.setValue(routeTiersPath, next as PathValue<TFieldValues, Path<TFieldValues>>, formSetOptions);
    cleanupPreferredChannel(next);
  };

  const setTierValue = (tierIndex: number, nextTier: RouteTierValue) => {
    const next = tiers.map((tier, index) => (index === tierIndex ? nextTier : tier));
    setTiers(next);
  };

  const addTier = () => {
    setTiers([
      ...tiers,
      {
        name: t('apikeys.profiles.routeTierDefaultName', { number: tiers.length + 1 }),
        channelIDs: [],
      },
    ]);
  };

  const removeTier = (tierIndex: number) => {
    setTiers(tiers.filter((_, index) => index !== tierIndex));
  };

  const moveTier = (tierIndex: number, direction: -1 | 1) => {
    const targetIndex = tierIndex + direction;
    if (targetIndex < 0 || targetIndex >= tiers.length) {
      return;
    }
    const next = [...tiers];
    [next[tierIndex], next[targetIndex]] = [next[targetIndex], next[tierIndex]];
    setTiers(next);
  };

  const setTierName = (tierIndex: number, name: string) => {
    setTierValue(tierIndex, {
      ...tiers[tierIndex],
      name,
      channelIDs: tiers[tierIndex]?.channelIDs ?? [],
    });
  };

  const setTierChannelIDs = (tierIndex: number, channelIDs: number[]) => {
    setTierValue(tierIndex, {
      ...tiers[tierIndex],
      channelIDs,
    });
  };

  const addChannel = (tierIndex: number, channelID: number) => {
    const current = tiers[tierIndex]?.channelIDs ?? [];
    if (current.includes(channelID)) {
      return;
    }
    setTierChannelIDs(tierIndex, [...current, channelID]);
    setSelectResetKey((value) => value + 1);
  };

  const removeChannel = (tierIndex: number, channelIndex: number) => {
    const current = tiers[tierIndex]?.channelIDs ?? [];
    setTierChannelIDs(
      tierIndex,
      current.filter((_, index) => index !== channelIndex)
    );
  };

  const moveChannel = (tierIndex: number, channelIndex: number, direction: -1 | 1) => {
    const current = tiers[tierIndex]?.channelIDs ?? [];
    const targetIndex = channelIndex + direction;
    if (targetIndex < 0 || targetIndex >= current.length) {
      return;
    }
    const next = [...current];
    [next[channelIndex], next[targetIndex]] = [next[targetIndex], next[channelIndex]];
    setTierChannelIDs(tierIndex, next);
  };

  const setPreferredChannelID = (value: string) => {
    form.setValue(
      preferredChannelPath,
      (value === 'none' ? null : Number.parseInt(value, 10)) as PathValue<TFieldValues, Path<TFieldValues>>,
      formSetOptions
    );
  };

  return (
    <div className='border-t pt-6'>
      <div className='mb-3 flex items-start justify-between gap-3'>
        <div>
          <h4 className='text-sm font-medium'>{t('apikeys.profiles.routeTiersTitle')}</h4>
          <p className='text-muted-foreground mt-1 text-xs'>{t('apikeys.profiles.routeTiersDescription')}</p>
        </div>
        <Button type='button' variant='outline' size='sm' onClick={addTier} className='shrink-0'>
          <IconPlus className='mr-2 h-4 w-4' />
          {t('apikeys.profiles.addRouteTier')}
        </Button>
      </div>

      <div className='space-y-3'>
        {tiers.length === 0 && (
          <div className='text-muted-foreground rounded-md border border-dashed p-4 text-center text-sm'>
            {t('apikeys.profiles.noRouteTiers')}
          </div>
        )}

        {tiers.map((tier, tierIndex) => {
          const selectedIDs = tier.channelIDs ?? [];
          const availableToAdd = channelOptions.filter((channel) => !selectedIDs.includes(channel.id));

          return (
            <div key={tierIndex} className='rounded-md border p-3'>
              <div className='flex flex-col gap-2 md:flex-row md:items-center'>
                <Input
                  value={tier.name ?? ''}
                  onChange={(event) => setTierName(tierIndex, event.target.value)}
                  placeholder={t('apikeys.profiles.routeTierName')}
                  className='md:w-56'
                />
                <div className='flex items-center gap-1 md:ml-auto'>
                  <Button type='button' variant='ghost' size='icon' onClick={() => moveTier(tierIndex, -1)} disabled={tierIndex === 0}>
                    <IconArrowUp className='h-4 w-4' />
                  </Button>
                  <Button type='button' variant='ghost' size='icon' onClick={() => moveTier(tierIndex, 1)} disabled={tierIndex === tiers.length - 1}>
                    <IconArrowDown className='h-4 w-4' />
                  </Button>
                  <Button type='button' variant='ghost' size='icon' onClick={() => removeTier(tierIndex)} className='text-destructive hover:text-destructive'>
                    <IconTrash className='h-4 w-4' />
                  </Button>
                </div>
              </div>

              <div className='mt-3 space-y-2'>
                {selectedIDs.length === 0 ? (
                  <div className='text-destructive rounded-md border border-dashed p-3 text-sm'>
                    {t('apikeys.profiles.noRouteChannels')}
                  </div>
                ) : (
                  selectedIDs.map((channelID, channelIndex) => {
                    const channel = channelByID.get(channelID);
                    return (
                      <div key={`${channelID}-${channelIndex}`} className='bg-muted/30 flex items-center gap-2 rounded-md border px-2 py-1.5'>
                        <div className='min-w-0 flex-1'>
                          <div className='truncate text-sm font-medium'>{channel?.name ?? t('apikeys.profiles.unknownChannel')}</div>
                          <div className='text-muted-foreground text-xs'>{channel?.type ?? '-'}</div>
                        </div>
                        <Button type='button' variant='ghost' size='icon' onClick={() => moveChannel(tierIndex, channelIndex, -1)} disabled={channelIndex === 0}>
                          <IconArrowUp className='h-4 w-4' />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          onClick={() => moveChannel(tierIndex, channelIndex, 1)}
                          disabled={channelIndex === selectedIDs.length - 1}
                        >
                          <IconArrowDown className='h-4 w-4' />
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          onClick={() => removeChannel(tierIndex, channelIndex)}
                          className='text-destructive hover:text-destructive'
                        >
                          <IconTrash className='h-4 w-4' />
                        </Button>
                      </div>
                    );
                  })
                )}

                <Select
                  key={`${tierIndex}-${selectResetKey}`}
                  onValueChange={(value) => {
                    addChannel(tierIndex, Number.parseInt(value, 10));
                  }}
                  disabled={availableToAdd.length === 0}
                >
                  <SelectTrigger className='w-full'>
                    <SelectValue placeholder={t('apikeys.profiles.routeTierChannels')} />
                  </SelectTrigger>
                  <SelectContent>
                    {availableToAdd.map((channel) => (
                      <SelectItem key={channel.id} value={channel.id.toString()}>
                        {channel.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
          );
        })}
      </div>

      <div className='mt-4'>
        <label className='mb-2 block text-sm font-medium'>{t('apikeys.profiles.preferredChannel')}</label>
        <Select value={preferredChannelID == null ? 'none' : preferredChannelID.toString()} onValueChange={setPreferredChannelID}>
          <SelectTrigger className='w-full'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='none'>{t('apikeys.profiles.noPreferredChannel')}</SelectItem>
            {routeChannelIDs.map((channelID) => (
              <SelectItem key={channelID} value={channelID.toString()}>
                {channelByID.get(channelID)?.name ?? t('apikeys.profiles.unknownChannel')}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
}
