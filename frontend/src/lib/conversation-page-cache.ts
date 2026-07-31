import type {
  ConversationEventPage,
  ConversationTurnHistoryPage,
  ConversationTurnPage,
} from "./types";

export interface PageCache<T> {
  values: Map<string, T>;
  pending: Map<string, { generation: number; promise: Promise<T> }>;
  generation: number;
}

export interface ConversationPageCache {
  events: PageCache<ConversationEventPage>;
  turnHistory: PageCache<ConversationTurnHistoryPage>;
  turnSummaries: PageCache<ConversationTurnPage>;
}

function createPageCache<T>(): PageCache<T> {
  return {
    values: new Map(),
    pending: new Map(),
    generation: 0,
  };
}

export function createConversationPageCache(): ConversationPageCache {
  return {
    events: createPageCache(),
    turnHistory: createPageCache(),
    turnSummaries: createPageCache(),
  };
}

export async function loadCachedPage<T>(
  cache: PageCache<T>,
  key: string,
  loader: () => Promise<T>,
) {
  const cached = cache.values.get(key);
  if (cached) return cached;

  const pending = cache.pending.get(key);
  if (pending) return pending.promise;

  const generation = cache.generation;
  const promise = loader()
    .then((page) => {
      if (cache.generation === generation) cache.values.set(key, page);
      return page;
    })
    .finally(() => {
      const current = cache.pending.get(key);
      if (current?.generation === generation) cache.pending.delete(key);
    });
  cache.pending.set(key, { generation, promise });
  return promise;
}

export function invalidateConversationPageCache(cache: ConversationPageCache) {
  for (const pageCache of [cache.events, cache.turnHistory, cache.turnSummaries]) {
    pageCache.generation += 1;
    pageCache.values.clear();
    pageCache.pending.clear();
  }
}
