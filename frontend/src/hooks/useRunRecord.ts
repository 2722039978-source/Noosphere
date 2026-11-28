import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { isRunActive } from '@/lib/task-client';
import { readRunSnapshot, type RunSnapshot } from '@/lib/run-observation';

export function useRunRecord(workspace: string, id: string, enabled: boolean) {
  const cache = useQueryClient();
  const key = ['run-observation', workspace, id];
  return useQuery({
    queryKey: key, enabled: enabled && !!workspace && !!id,
    queryFn: () => readRunSnapshot(api, workspace, id, cache.getQueryData<RunSnapshot>(key)),
    retry: 2, refetchIntervalInBackground: true,
    refetchInterval: query => {
      if (query.state.error) return 5000;
      const data = query.state.data;
      if (data?.more) return 50;
      return data && isRunActive(data.run) ? 1400 : false;
    },
  });
}
