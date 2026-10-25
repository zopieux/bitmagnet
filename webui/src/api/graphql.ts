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
  totalCountIsEstimate?: boolean;
  hasNextPage?: boolean;
  items: TorrentContentItem[];
  aggregations?: TorrentSearchAggregations;
}

export interface TorrentFileItem {
  path: string;
  size: number;
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
  aggregate?: boolean;
}

export async function searchTorrents(options: SearchOptions): Promise<TorrentSearchResult> {
  const shouldAggregate = options.aggregate ?? (options.page === undefined || options.page <= 1);
  const facets: Record<string, any> = {
    contentType: { aggregate: shouldAggregate },
    torrentTag: { aggregate: shouldAggregate },
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
          totalCountIsEstimate
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

export async function archiveTorrents(infoHashes: string[]): Promise<void> {
  const query = `
    mutation ArchiveTorrents($infoHashes: [Hash20!]!) {
      torrent {
        archive(infoHashes: $infoHashes)
      }
    }
  `;
  await graphqlFetch(query, { infoHashes });
}


