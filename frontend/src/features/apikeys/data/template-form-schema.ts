import { z } from 'zod';

export const formSchemaFactory = (t: (key: string) => string) =>
  z
    .object({
      name: z.string().min(1, t('apikeys.templates.templateNameRequired')),
      description: z.string().optional(),
      profile: z.object({
        name: z.string().min(1, t('apikeys.validation.profileNameRequired')),
        modelMappings: z.array(
          z.object({
            from: z.string().min(1, t('apikeys.validation.sourceModelRequired')),
            to: z.string().min(1, t('apikeys.validation.targetModelRequired')),
          })
        ),
        routeTiers: z
          .array(
            z.object({
              name: z.string().optional().default(''),
              channelIDs: z.array(z.number()).min(1, t('apikeys.validation.routeTierChannelsRequired')),
            })
          )
          .min(1, t('apikeys.validation.routeTiersRequired')),
        preferredChannelID: z.number().optional().nullable(),
        modelIDs: z.array(z.string()).optional().nullable(),
        quota: z
          .object({
            requests: z.number().int().positive().optional().nullable(),
            totalTokens: z.number().int().positive().optional().nullable(),
            cost: z.number().optional().nullable(),
            period: z.object({
              type: z.enum(['all_time', 'past_duration', 'calendar_duration']),
              pastDuration: z
                .object({
                  value: z.number().int().positive(),
                  unit: z.enum(['minute', 'hour', 'day']),
                })
                .optional()
                .nullable(),
              calendarDuration: z
                .object({
                  unit: z.enum(['day', 'month']),
                })
                .optional()
                .nullable(),
            }),
          })
          .optional()
          .nullable(),
      }),
    })
    .superRefine((data, ctx) => {
      const quota = data.profile?.quota;
      const routeChannelIDs = new Set(data.profile.routeTiers.flatMap((tier) => tier.channelIDs ?? []));
      if (data.profile.preferredChannelID != null && !routeChannelIDs.has(data.profile.preferredChannelID)) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('apikeys.validation.preferredChannelMustBeInRoute'),
          path: ['profile', 'preferredChannelID'],
        });
      }

      if (!quota) return;

      const requests = quota.requests ?? undefined;
      const totalTokens = quota.totalTokens ?? undefined;
      const cost = quota.cost ?? undefined;

      if (requests == null && totalTokens == null && cost == null) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('apikeys.validation.quotaAtLeastOneLimit'),
          path: ['profile', 'quota'],
        });
      }

      if (quota.period.type === 'past_duration' && !quota.period.pastDuration) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('apikeys.validation.quotaPastDurationRequired'),
          path: ['profile', 'quota', 'period', 'pastDuration'],
        });
      }

      if (quota.period.type === 'calendar_duration' && !quota.period.calendarDuration) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: t('apikeys.validation.quotaCalendarDurationRequired'),
          path: ['profile', 'quota', 'period', 'calendarDuration'],
        });
      }
    });

export type FormValues = z.infer<ReturnType<typeof formSchemaFactory>>;
