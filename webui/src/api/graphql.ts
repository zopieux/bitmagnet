export interface TorrentSource {
  key: string;
  name: string;
}

export interface TorrentItem {
  infoHash: string;
  name: string;
  size: number;
  filesCount?: number;
  filesStatus?: string;
  fileType?: string;
  sources?: TorrentSource[];
  seeders?: number;
  leechers?: number;
  tagNames?: string[];
  magnetUri?: string;
}

export interface ContentItem {
  type?: string;
  source?: string;
  id?: string;
  title?: string;
  releaseDate?: string;
  releaseYear?: number;
  overview?: string;
}

export interface TorrentContentItem {
  id: string;
  infoHash: string;
  contentType?: string;
  title?: string;
  seeders?: number;
  leechers?: number;
  publishedAt?: string;
  torrent: TorrentItem;
  content?: ContentItem;
}

export interface AggregationItem {
  value: string;
  label: string;
  count: number;
}

export interface TorrentSearchAggregations {
  contentType?: AggregationItem[];
  torrentTag?: AggregationItem[];
  torrentFileType?: AggregationItem[];
}

export interface TorrentSearchResult {
  totalCount: number;
  hasNextPage?: boolean;
  items: TorrentContentItem[];
  aggregations?: TorrentSearchAggregations;
}

export interface TorrentFileItem {
  path: string;
  size: number;
}

export interface MetricTimePoint {
  timestamp: string;
  label: string;
  discovered: number;
  processed: number;
  failing: number;
}

export type TimeframeMode = 'hourly' | 'daily' | 'weekly';

export interface AdminDashboardMetrics {
  totalProcessedTorrents: number;
  pendingQueueSize: number;
  retryQueueSize: number;
  discoveredRatePerMin: number;
  processedRatePerMin: number;
  failingRatePerMin: number;
  historyPoints: MetricTimePoint[];
}

async function graphqlFetch<T>(query: string, variables: Record<string, any> = {}): Promise<T> {
  const response = await fetch('/graphql', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    },
    body: JSON.stringify({ query, variables }),
  });

  if (!response.ok) {
    throw new Error(`GraphQL HTTP error! status: ${response.status}`);
  }

  const result = await response.json();
  if (result.errors && result.errors.length > 0) {
    throw new Error(result.errors[0].message || 'GraphQL error');
  }

  return result.data;
}

export interface SearchOptions {
  queryString?: string;
  page?: number;
  limit?: number;
  contentTypes?: string[];
  tags?: string[];
  orderBy?: { field: string; descending: boolean };
}

export async function searchTorrents(options: SearchOptions): Promise<TorrentSearchResult> {
  const facets: Record<string, any> = {
    contentType: { aggregate: true },
    torrentTag: { aggregate: true },
  };

  if (options.contentTypes && options.contentTypes.length > 0) {
    facets.contentType.filter = options.contentTypes;
  }

  if (options.tags && options.tags.length > 0) {
    facets.torrentTag.filter = options.tags;
  }

  const query = `
    query SearchTorrents($input: TorrentContentSearchQueryInput!) {
      torrentContent {
        search(input: $input) {
          totalCount
          hasNextPage
          items {
            id
            infoHash
            contentType
            title
            seeders
            leechers
            publishedAt
            torrent {
              infoHash
              name
              size
              filesCount
              filesStatus
              fileType
              sources {
                key
                name
              }
              seeders
              leechers
              tagNames
              magnetUri
              }
            content {
              type
              source
              id
              title
              releaseDate
              releaseYear
              overview
            }
          }
          aggregations {
            contentType {
              value
              label
              count
            }
            torrentTag {
              value
              label
              count
            }
          }
        }
      }
    }
  `;

  const input: Record<string, any> = {
    limit: options.limit || 20,
    page: options.page || 1,
    totalCount: true,
    hasNextPage: true,
    facets,
  };

  if (options.queryString && options.queryString.trim() !== '') {
    input.queryString = options.queryString.trim();
  }

  if (options.orderBy) {
    input.orderBy = [{ field: options.orderBy.field, descending: options.orderBy.descending }];
  }

  const data = await graphqlFetch<{ torrentContent: { search: TorrentSearchResult } }>(query, { input });
  return data.torrentContent.search;
}

export interface TorrentFilesPage {
  items: TorrentFileItem[];
  totalCount: number;
  hasNextPage: boolean;
}

export interface TorrentFilesOptions {
  query?: string;
  orderBy?: { field: 'path' | 'size'; descending: boolean }[];
}

export async function getTorrentFilesPage(
  infoHash: string,
  page: number,
  limit: number,
  opts?: TorrentFilesOptions
): Promise<TorrentFilesPage> {
  const query = `
    query GetTorrentFiles($input: TorrentFilesQueryInput!) {
      torrent {
        files(input: $input) {
          totalCount
          hasNextPage
          items {
            path
            size
          }
        }
      }
    }
  `;

  const input: Record<string, unknown> = {
    infoHashes: [infoHash],
    page,
    limit,
    totalCount: true,
    hasNextPage: true,
  };
  if (opts?.query) input.query = opts.query;
  if (opts?.orderBy?.length) input.orderBy = opts.orderBy;

  const data = await graphqlFetch<{ torrent: { files: TorrentFilesPage } }>(query, { input });
  return data.torrent.files;
}

export async function putTorrentTags(infoHashes: string[], tagNames: string[]): Promise<void> {
  const query = `
    mutation PutTags($infoHashes: [Hash20!]!, $tagNames: [String!]!) {
      torrent {
        putTags(infoHashes: $infoHashes, tagNames: $tagNames)
      }
    }
  `;
  await graphqlFetch(query, { infoHashes, tagNames });
}

export async function setTorrentTags(infoHashes: string[], tagNames: string[]): Promise<void> {
  const query = `
    mutation SetTags($infoHashes: [Hash20!]!, $tagNames: [String!]!) {
      torrent {
        setTags(infoHashes: $infoHashes, tagNames: $tagNames)
      }
    }
  `;
  await graphqlFetch(query, { infoHashes, tagNames });
}

export async function deleteTorrentTags(infoHashes: string[], tagNames: string[]): Promise<void> {
  const query = `
    mutation DeleteTags($infoHashes: [Hash20!], $tagNames: [String!]) {
      torrent {
        deleteTags(infoHashes: $infoHashes, tagNames: $tagNames)
      }
    }
  `;
  await graphqlFetch(query, { infoHashes, tagNames });
}

export async function deleteTorrents(infoHashes: string[]): Promise<void> {
  const query = `
    mutation DeleteTorrents($infoHashes: [Hash20!]!) {
      torrent {
        delete(infoHashes: $infoHashes)
      }
    }
  `;
  await graphqlFetch(query, { infoHashes });
}

export async function getAdminMetrics(timeframe: TimeframeMode = 'hourly'): Promise<AdminDashboardMetrics> {
  const now = new Date();
  const startTime5m = new Date(now.getTime() - 5 * 60 * 1000).toISOString();

  // Determine history query params and interval size for exactly 96 data points
  let historyBucketDuration: 'minute' | 'hour' | 'day' = 'minute';
  let totalDurationMs = 24 * 60 * 60 * 1000; // 24 hours for 'hourly'
  let stepMs = 15 * 60 * 1000; // 15 mins for 'hourly'

  if (timeframe === 'daily') {
    historyBucketDuration = 'hour';
    totalDurationMs = 96 * 60 * 60 * 1000; // 96 hours (4 days)
    stepMs = 60 * 60 * 1000; // 1 hour steps
  } else if (timeframe === 'weekly') {
    historyBucketDuration = 'hour';
    totalDurationMs = 96 * 7 * 60 * 60 * 1000; // 28 days (672 hours)
    stepMs = 7 * 60 * 60 * 1000; // 7 hour steps
  }

  const startTimeHistory = new Date(now.getTime() - totalDurationMs).toISOString();

  const query = `
    query AdminMetrics(
      $queueInput: QueueMetricsQueryInput!,
      $torrentInput: TorrentMetricsQueryInput!,
      $queueHistoryInput: QueueMetricsQueryInput!,
      $torrentHistoryInput: TorrentMetricsQueryInput!,
      $jobsInput: QueueJobsQueryInput!,
      $searchCountInput: TorrentContentSearchQueryInput!
    ) {
      torrentContent {
        search(input: $searchCountInput) {
          totalCount
        }
      }
      queue {
        jobs(input: $jobsInput) {
          aggregations {
            status {
              value
              count
            }
          }
        }
        metrics(input: $queueInput) {
          buckets {
            queue
            status
            count
          }
        }
        metricsHistory: metrics(input: $queueHistoryInput) {
          buckets {
            bucket
            status
            count
          }
        }
      }
      torrent {
        metrics(input: $torrentInput) {
          buckets {
            source
            updated
            count
          }
        }
        metricsHistory: metrics(input: $torrentHistoryInput) {
          buckets {
            bucket
            count
          }
        }
      }
    }
  `;

  const variables = {
    searchCountInput: { limit: 0, totalCount: true },
    jobsInput: { limit: 0, totalCount: false, facets: { status: { aggregate: true } } },
    queueInput: { bucketDuration: 'minute', startTime: startTime5m },
    torrentInput: { bucketDuration: 'minute', startTime: startTime5m },
    queueHistoryInput: { bucketDuration: historyBucketDuration, startTime: startTimeHistory },
    torrentHistoryInput: { bucketDuration: historyBucketDuration, startTime: startTimeHistory },
  };

  try {
    const data = await graphqlFetch<{
      torrentContent: { search: { totalCount: number } };
      queue: {
        jobs: { aggregations?: { status?: { value: string; count: number }[] } };
        metrics: { buckets?: { queue: string; status: string; count: number }[] };
        metricsHistory: { buckets?: { bucket: string; status: string; count: number }[] };
      };
      torrent: {
        metrics: { buckets?: { source: string; updated: boolean; count: number }[] };
        metricsHistory: { buckets?: { bucket: string; count: number }[] };
      };
    }>(query, variables);

    const totalProcessedTorrents = data.torrentContent?.search?.totalCount || 0;

    let pendingQueueSize = 0;
    let retryQueueSize = 0;
    const statusAggs = data.queue?.jobs?.aggregations?.status || [];
    for (const agg of statusAggs) {
      if (agg.value === 'pending') pendingQueueSize = agg.count;
      if (agg.value === 'retry') retryQueueSize = agg.count;
    }

    let discoveredRatePerMin = 0;
    const torrentBuckets = data.torrent?.metrics?.buckets || [];
    for (const b of torrentBuckets) {
      discoveredRatePerMin += b.count || 0;
    }

    let processedRatePerMin = 0;
    let failingRatePerMin = 0;
    const queueBuckets = data.queue?.metrics?.buckets || [];
    for (const b of queueBuckets) {
      if (b.status === 'processed') processedRatePerMin += b.count || 0;
      if (b.status === 'failed' || b.status === 'retry') failingRatePerMin += b.count || 0;
    }

    // Build 96 discrete time points initialized to zero
    const points: { tsMs: number; discovered: number; processed: number; failing: number }[] = [];
    const nowMs = now.getTime();
    for (let i = 95; i >= 0; i--) {
      points.push({
        tsMs: nowMs - i * stepMs,
        discovered: 0,
        processed: 0,
        failing: 0,
      });
    }

    // Map raw buckets to the nearest 96 time point
    const tHistory = data.torrent?.metricsHistory?.buckets || [];
    for (const b of tHistory) {
      if (b.bucket) {
        const bTime = new Date(b.bucket).getTime();
        // Find closest point index
        let bestIdx = 0;
        let minDiff = Math.abs(points[0].tsMs - bTime);
        for (let idx = 1; idx < points.length; idx++) {
          const diff = Math.abs(points[idx].tsMs - bTime);
          if (diff < minDiff) {
            minDiff = diff;
            bestIdx = idx;
          }
        }
        points[bestIdx].discovered += b.count || 0;
      }
    }

    const qHistory = data.queue?.metricsHistory?.buckets || [];
    for (const b of qHistory) {
      if (b.bucket) {
        const bTime = new Date(b.bucket).getTime();
        let bestIdx = 0;
        let minDiff = Math.abs(points[0].tsMs - bTime);
        for (let idx = 1; idx < points.length; idx++) {
          const diff = Math.abs(points[idx].tsMs - bTime);
          if (diff < minDiff) {
            minDiff = diff;
            bestIdx = idx;
          }
        }
        if (b.status === 'processed') {
          points[bestIdx].processed += b.count || 0;
        } else if (b.status === 'failed' || b.status === 'retry') {
          points[bestIdx].failing += b.count || 0;
        }
      }
    }

    const historyPoints: MetricTimePoint[] = points.map((p) => {
      const d = new Date(p.tsMs);
      let label = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
      if (timeframe !== 'hourly') {
        label = `${d.getMonth() + 1}/${d.getDate()} ${label}`;
      }
      return {
        timestamp: d.toISOString(),
        label,
        discovered: p.discovered,
        processed: p.processed,
        failing: p.failing,
      };
    });

    return {
      totalProcessedTorrents,
      pendingQueueSize,
      retryQueueSize,
      discoveredRatePerMin,
      processedRatePerMin,
      failingRatePerMin,
      historyPoints,
    };
  } catch (err) {
    console.error('Failed to fetch admin metrics:', err);
    return {
      totalProcessedTorrents: 0,
      pendingQueueSize: 0,
      retryQueueSize: 0,
      discoveredRatePerMin: 0,
      processedRatePerMin: 0,
      failingRatePerMin: 0,
      historyPoints: [],
    };
  }
}
