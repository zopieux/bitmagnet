import { createSignal, createEffect, onMount, onCleanup, For, Show, Component } from 'solid-js';
import {
  searchTorrents,
  getTorrentFilesPage,
  putTorrentTags,
  setTorrentTags,
  deleteTorrentTags,
  deleteTorrents,
  archiveTorrents,
  TorrentContentItem,
  TorrentFileItem,
  AggregationItem,
} from '../api/graphql';
import { MagnetIcon } from './MagnetIcon';

function formatSize(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${Math.round(bytes / Math.pow(k, i))} ${sizes[i]}`;
}

// Relative age for a discovery timestamp, e.g. "3d ago".
function formatAge(iso: string): string {
  const then = new Date(iso).getTime();
  if (isNaN(then)) return '';
  const s = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (s < 60) return 'just now';
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d}d ago`;
  const mo = Math.floor(d / 30);
  if (mo < 12) return `${mo}mo ago`;
  return `${Math.floor(mo / 12)}y ago`;
}

// A collapsible region that stays in the DOM with hidden="until-found" while
// closed, so browser find-in-page can match its contents; the beforematch
// event then opens it via onBeforeMatch.
const Disclosure: Component<{ open: boolean; onBeforeMatch: () => void; children?: any }> = (props) => {
  let el: HTMLDivElement | undefined;
  createEffect(() => {
    if (!el) return;
    if (props.open) {
      el.removeAttribute('hidden');
    } else {
      el.setAttribute('hidden', 'until-found');
    }
  });
  return (
    <div
      ref={(e) => {
        el = e;
        e.addEventListener('beforematch', () => props.onBeforeMatch());
      }}
    >
      {props.children}
    </div>
  );
};

// File browser shown inside an expanded torrent row: sort buttons and a
// filename filter (both applied server-side through the files query), the
// file table, and its pager. Renders two cells (controls, table) of the
// expanded row's grid; the tags editor occupies the remaining cell.
const FilesPanel: Component<{ hash: string; active: boolean; isSingleFile?: boolean; tagsSlot?: any }> = (props) => {
  const PAGE_SIZE = 10;
  const [page, setPage] = createSignal(1);
  const [data, setData] = createSignal<{ items: TorrentFileItem[]; totalCount: number; hasNextPage: boolean } | null>(null);
  const [loading, setLoading] = createSignal(false);
  const [searchText, setSearchText] = createSignal('');
  const [appliedQuery, setAppliedQuery] = createSignal('');
  const [sortField, setSortField] = createSignal<'path' | 'size' | null>(null);
  const [sortDesc, setSortDesc] = createSignal(false);
  let loaded = false;
  let debounce: number | undefined;

  const isSingle = () =>
    props.isSingleFile || (data() != null && data()!.totalCount <= 1);

  const load = async (p: number) => {
    setLoading(true);
    try {
      setData(
        await getTorrentFilesPage(props.hash, p, PAGE_SIZE, {
          query: appliedQuery() || undefined,
          orderBy: sortField() ? [{ field: sortField()!, descending: sortDesc() }] : undefined,
        })
      );
    } catch (err) {
      console.error('Error fetching files:', err);
    } finally {
      setLoading(false);
    }
  };

  createEffect(() => {
    if (props.active && !loaded) {
      loaded = true;
      load(1);
    }
  });

  const totalPages = () => Math.max(1, Math.ceil((data()?.totalCount ?? 0) / PAGE_SIZE));
  const goto = (p: number) => {
    setPage(p);
    load(p);
  };

  const toggleSort = (field: 'path' | 'size') => {
    if (sortField() === field) {
      setSortDesc(!sortDesc());
    } else {
      setSortField(field);
      setSortDesc(false);
    }
    setPage(1);
    load(1);
  };

  const onSearchInput = (v: string) => {
    setSearchText(v);
    window.clearTimeout(debounce);
    debounce = window.setTimeout(() => {
      setAppliedQuery(v.trim());
      setPage(1);
      load(1);
    }, 300);
  };

  const sortMark = (field: 'path' | 'size') =>
    sortField() === field ? (sortDesc() ? ' ▼' : ' ▲') : '';

  return (
    <>
      <div
        class={`expanded-row-toolbar ${isSingle() ? 'single-file' : ''}`}
        onClick={(e) => e.stopPropagation()}
      >
        <Show when={!isSingle()}>
          <div class="search-sort-group">
            <input
              class="input-sm filename-filter-input"
              type="text"
              placeholder="Filter by filename..."
              value={searchText()}
              onInput={(e) => onSearchInput(e.currentTarget.value)}
              onClick={(e) => e.stopPropagation()}
            />
            <button
              class={`nav-btn btn-sm ${sortField() === 'path' ? 'active' : ''}`}
              onClick={() => toggleSort('path')}
            >
              Filename{sortMark('path')}
            </button>
            <button
              class={`nav-btn btn-sm ${sortField() === 'size' ? 'active' : ''}`}
              onClick={() => toggleSort('size')}
            >
              Size{sortMark('size')}
            </button>
          </div>
        </Show>
        {props.tagsSlot}
      </div>
      <Show when={(loading() && !data()) || (data() && (data()!.totalCount > 0 || appliedQuery()))}>
        <div class="cell-table" style={{ padding: '0 1rem 0.4rem' }}>
          <Show when={loading() && !data()}>
            <div style={{ 'font-size': '0.82rem', color: 'var(--app-text-muted)' }}>Loading files...</div>
          </Show>
          <Show when={data() && data()!.totalCount === 0 && appliedQuery()}>
            <div style={{ 'font-size': '0.82rem', color: 'var(--app-text-muted)' }}>
              No files match "{appliedQuery()}".
            </div>
          </Show>
          <Show when={data() && data()!.totalCount > 0}>
            <table class="compact-table" style={{ margin: 0, 'margin-bottom': 0, 'border-radius': 0 }}>
              <thead>
                <tr>
                  <th class="torrent-file-col">File Path</th>
                  <th style={{ width: '110px', 'text-align': 'right' }}>Size</th>
                </tr>
              </thead>
              <tbody>
                <For each={data()!.items}>
                  {(file) => (
                    <tr>
                      <td class="torrent-file-col" title={file.path}>{file.path}</td>
                      <td style={{ 'white-space': 'nowrap', 'text-align': 'right', 'font-size': '0.82rem' }} title={String(file.size)}>{formatSize(file.size)}</td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
            <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-top': '0.35rem' }}>
              <span style={{ 'font-size': '0.78rem', color: 'var(--app-text-muted)' }}>
                {totalPages() > 1 ? `${(page() - 1) * PAGE_SIZE + 1}–${Math.min(page() * PAGE_SIZE, data()!.totalCount)} of ` : ''}
                {data()!.totalCount} {data()!.totalCount === 1 ? 'file' : 'files'}
              </span>
              <Show when={totalPages() > 1}>
                <span style={{ display: 'flex', 'align-items': 'center', gap: '0.5rem' }}>
                  <button class="nav-btn" disabled={page() <= 1 || loading()} onClick={() => goto(page() - 1)}>
                    ← Prev
                  </button>
                  <span style={{ 'font-size': '0.78rem', color: 'var(--app-text-muted)' }}>
                    Page <strong style={{ color: 'var(--app-text)' }}>{page()}</strong> of <strong style={{ color: 'var(--app-text)' }}>{totalPages()}</strong>
                  </span>
                  <button class="nav-btn" disabled={!data()!.hasNextPage || loading()} onClick={() => goto(page() + 1)}>
                    Next →
                  </button>
                </span>
              </Show>
            </div>
          </Show>
        </div>
      </Show>
    </>
  );
};

// Inline tag editor: chips + input, saved automatically on every change.
// Keyboard model (when not editing a chip): ArrowLeft/ArrowRight move focus
// between chips and the input; Enter on a chip starts editing it; Delete on a
// chip removes it. In the input (new tag, or while editing a chip), Space or
// Enter commits the tag; Escape cancels an edit.
const TagEditor: Component<{ tags: string[]; onSave: (tags: string[]) => void; style?: Record<string, string> }> = (props) => {
  const [editingIdx, setEditingIdx] = createSignal(-1);
  const [editText, setEditText] = createSignal('');
  const [newText, setNewText] = createSignal('');
  let inputRef: HTMLInputElement | undefined;
  let editRef: HTMLInputElement | undefined;
  let editorRef: HTMLDivElement | undefined;

  const normalize = (t: string) => t.trim().toLowerCase();
  // Focus chips by live DOM position: after a removal the chip elements
  // shift, so cached element references go stale and focus is lost.
  const focusChip = (i: number) => {
    editorRef?.querySelectorAll<HTMLElement>('.tag-chip')[i]?.focus();
  };
  const focusInput = () => inputRef?.focus();

  const commitNew = () => {
    const t = normalize(newText());
    if (!t) return;
    setNewText('');
    if (!props.tags.includes(t)) {
      props.onSave([...props.tags, t]);
    }
  };

  const startEdit = (i: number) => {
    setEditingIdx(i);
    setEditText(props.tags[i]);
    setTimeout(() => {
      editRef?.focus();
      editRef?.select();
    }, 0);
  };

  const commitEdit = () => {
    const i = editingIdx();
    if (i < 0) return;
    const t = normalize(editText());
    setEditingIdx(-1);
    if (t && t !== props.tags[i]) {
      const next = [...props.tags];
      next[i] = t;
      props.onSave([...new Set(next)]);
    }
    setTimeout(() => focusChip(Math.min(i, props.tags.length - 1)), 0);
  };

  const cancelEdit = () => {
    const i = editingIdx();
    setEditingIdx(-1);
    if (i >= 0) setTimeout(() => focusChip(i), 0);
  };

  const removeTag = (i: number) => {
    const next = props.tags.filter((_, j) => j !== i);
    props.onSave(next);
    setTimeout(() => {
      // Select the tag that slid into the removed tag's place; if the
      // removed tag was the last one, fall back to the add-tag input.
      if (i < next.length) focusChip(i);
      else focusInput();
    }, 0);
  };

  const onEditorKeyDown = (e: KeyboardEvent) => {
    if (editingIdx() >= 0) return; // the edit input handles its own keys
    const chipEl = (e.target as HTMLElement).closest?.('.tag-chip') as HTMLElement | null;
    if (chipEl) {
      const i = Number(chipEl.dataset.idx);
      if (e.key === 'ArrowLeft') {
        e.preventDefault();
        if (i > 0) focusChip(i - 1);
      } else if (e.key === 'ArrowRight') {
        e.preventDefault();
        if (i < props.tags.length - 1) focusChip(i + 1);
        else focusInput();
      } else if (e.key === 'Enter') {
        e.preventDefault();
        startEdit(i);
      } else if (e.key === 'Delete' || e.key === 'Backspace') {
        e.preventDefault();
        removeTag(i);
      }
      return;
    }
    if (e.target === inputRef) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        commitNew();
      } else if (e.key === 'ArrowLeft' && inputRef && inputRef.selectionStart === 0 && props.tags.length > 0) {
        e.preventDefault();
        focusChip(props.tags.length - 1);
      }
    }
  };

  return (
    <div
      class="tag-editor"
      ref={(e) => (editorRef = e)}
      onKeyDown={onEditorKeyDown}
      style={{ display: 'flex', 'align-items': 'center', gap: '0.35rem', ...props.style }}
    >
      <For each={props.tags}>
        {(tag, idx) => (
          <Show
            when={editingIdx() === idx()}
            fallback={
              <span
                class="badge tag-chip"
                data-idx={idx()}
                tabIndex={0}
                onClick={() => focusChip(idx())}
                onDblClick={() => startEdit(idx())}
              >
                {tag}
              </span>
            }
          >
            <input
              class="input-sm"
              type="text"
              value={editText()}
              ref={(el) => (editRef = el)}
              onInput={(e) => setEditText(e.currentTarget.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  commitEdit();
                } else if (e.key === 'Escape') {
                  e.preventDefault();
                  cancelEdit();
                }
                e.stopPropagation();
              }}
              onClick={(e) => e.stopPropagation()}
              style={{ width: '6rem' }}
            />
          </Show>
        )}
      </For>
      <input
        class="input-sm"
        type="text"
        placeholder="Add tag..."
        value={newText()}
        ref={(el) => (inputRef = el)}
        onInput={(e) => setNewText(e.currentTarget.value)}
        onClick={(e) => e.stopPropagation()}
        style={{ flex: '1 1 auto', 'min-width': '4.5rem' }}
      />
    </div>
  );
};

type ColumnKey = 'size' | 'peers' | 'category' | 'tags' | 'magnet';

const TOGGLABLE_COLUMNS: { key: ColumnKey; label: string }[] = [
  { key: 'size', label: 'Size' },
  { key: 'peers', label: 'Seeds / Leech' },
  { key: 'category', label: 'Category' },
  { key: 'tags', label: 'Tags' },
  { key: 'magnet', label: 'Copy magnet' },
];

const SORT_OPTIONS = [
  { field: 'name', label: 'Name' },
  { field: 'info_hash', label: 'Hash' },
  { field: 'published_at', label: 'Age' },
  { field: 'seeders', label: 'Seeds' },
  { field: 'leechers', label: 'Leech' },
  { field: 'size', label: 'Size' },
  { field: 'files_count', label: 'Files' },
] as const;

type SortField = typeof SORT_OPTIONS[number]['field'];

interface SortState {
  field: SortField;
  isAsc: boolean;
}

const DEFAULT_SORT: SortState = {
  field: 'published_at',
  isAsc: true,
};

function parseSortParam(raw: string | null): SortState {
  if (!raw) return DEFAULT_SORT;
  let field = raw.trim();
  let isAsc = true;
  if (field.startsWith('-')) {
    isAsc = false;
    field = field.slice(1).trim();
  } else if (field.startsWith('+') || field.startsWith(' ')) {
    isAsc = true;
    field = field.slice(1).trim();
  }
  if (!SORT_OPTIONS.some((o) => o.field === field)) return DEFAULT_SORT;
  return { field: field as SortField, isAsc };
}

function parseUrlState() {
  const params = new URLSearchParams(window.location.search);
  const q = params.get('q') || '';
  const pageRaw = parseInt(params.get('page') || '1', 10);
  const page = isNaN(pageRaw) || pageRaw < 1 ? 1 : pageRaw;
  const perPageRaw = parseInt(params.get('per_page') || params.get('limit') || '25', 10);
  const perPage = [10, 25, 50, 100].includes(perPageRaw) ? perPageRaw : 25;
  const categories = params.getAll('category').flatMap((c) => c.split(',')).filter(Boolean);
  const tags = params.getAll('tag').flatMap((t) => t.split(',')).filter(Boolean);
  const sort = parseSortParam(params.get('sort'));

  return { q, page, perPage, categories, tags, sort };
}

const MOBILE_COLS_STORAGE_KEY = 'bitmagnet_mobile_columns';

const loadInitialMobileCols = (): Set<ColumnKey> => {
  try {
    const saved = localStorage.getItem(MOBILE_COLS_STORAGE_KEY);
    if (saved) {
      const parsed = JSON.parse(saved);
      if (Array.isArray(parsed)) {
        const validKeys = new Set<string>(TOGGLABLE_COLUMNS.map((c) => c.key));
        const filtered = parsed.filter((k): k is ColumnKey => validKeys.has(k));
        return new Set(filtered);
      }
    }
  } catch (_) {}
  return new Set(['size', 'magnet']);
};

export const TorrentList: Component = () => {
  const initial = parseUrlState();
  const [queryString, setQueryString] = createSignal(initial.q);
  const [debouncedQuery, setDebouncedQuery] = createSignal(initial.q);
  const [sort, setSort] = createSignal<SortState>(initial.sort);
  const [selectedCategories, setSelectedCategories] = createSignal<string[]>(initial.categories);
  const [selectedTags, setSelectedTags] = createSignal<string[]>(initial.tags);
  const [page, setPage] = createSignal(initial.page);
  const [limit, setLimit] = createSignal(initial.perPage);

  const [items, setItems] = createSignal<TorrentContentItem[]>([]);
  const [totalCount, setTotalCount] = createSignal(0);
  const [totalCountIsEstimate, setTotalCountIsEstimate] = createSignal(false);
  const [hasNextPage, setHasNextPage] = createSignal(false);
  const [categoryAggs, setCategoryAggs] = createSignal<AggregationItem[]>([]);
  const [tagAggs, setTagAggs] = createSignal<AggregationItem[]>([]);
  const [loading, setLoading] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);

  // Selection
  const [selectedHashes, setSelectedHashes] = createSignal<string[]>([]);

  // Mobile Filter Pane State & Column Toggles
  const [mobileFilterOpen, setMobileFilterOpen] = createSignal(false);
  const [mobileVisibleCols, setMobileVisibleCols] = createSignal<Set<ColumnKey>>(
    loadInitialMobileCols()
  );
  const toggleCol = (key: ColumnKey) => {
    setMobileVisibleCols((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      try {
        localStorage.setItem(MOBILE_COLS_STORAGE_KEY, JSON.stringify(Array.from(next)));
      } catch (_) { }
      return next;
    });
  };

  // Row expansion (actions, tag editor, files), keyed by info hash
  const [expanded, setExpanded] = createSignal<string | null>(null);
  const [copiedHash, setCopiedHash] = createSignal<string | null>(null);
  const copyHash = (hash: string) => {
    navigator.clipboard?.writeText(hash).catch(() => { });
    setCopiedHash(hash);
    setTimeout(() => {
      if (copiedHash() === hash) setCopiedHash(null);
    }, 1500);
  };

  // Tag edits apply locally at once and persist through setTorrentTags
  const [tagOverrides, setTagOverrides] = createSignal<Record<string, string[]>>({});
  const tagsFor = (item: TorrentContentItem): string[] =>
    tagOverrides()[item.torrent.infoHash] ?? item.torrent.tagNames ?? [];
  const saveTags = async (hash: string, tags: string[]) => {
    setTagOverrides({ ...tagOverrides(), [hash]: tags });
    try {
      await setTorrentTags([hash], tags);
    } catch (err: any) {
      console.error('Error saving tags:', err);
      alert(`Tag save failed: ${err.message}`);
    }
  };

  // Tag Modal State
  const [tagModal, setTagModal] = createSignal<{
    open: boolean;
    mode: 'add' | 'set' | 'remove';
    hashes: string[];
    tags: string[];
  }>({ open: false, mode: 'add', hashes: [], tags: [] });

  // Delete Confirmation State
  const [deleteModal, setDeleteModal] = createSignal<{ open: boolean; hashes: string[] }>({
    open: false,
    hashes: [],
  });

  // Archive Confirmation State
  const [archiveModal, setArchiveModal] = createSignal<{ open: boolean; hashes: string[] }>({
    open: false,
    hashes: [],
  });

  const categories = [
    { value: 'movie', label: 'Movies' },
    { value: 'tv_show', label: 'TV Shows' },
    { value: 'music', label: 'Music' },
    { value: 'ebook', label: 'Ebooks' },
    { value: 'comic', label: 'Comics' },
    { value: 'audiobook', label: 'Audiobooks' },
    { value: 'software', label: 'Software' },
    { value: 'game', label: 'Games' },
    { value: 'xxx', label: 'Adult (XXX)' },
  ];

  const fetchTorrents = async () => {
    setLoading(true);
    setError(null);
    try {
      const s = sort();
      const apiDescending = s.field === 'published_at' ? s.isAsc : !s.isAsc;
      const res = await searchTorrents({
        queryString: debouncedQuery(),
        page: page(),
        limit: limit(),
        contentTypes: selectedCategories(),
        tags: selectedTags(),
        orderBy: { field: s.field, descending: apiDescending },
      });
      setItems(res.items || []);
      setTotalCount(res.totalCount || 0);
      setTotalCountIsEstimate(!!res.totalCountIsEstimate);
      setHasNextPage(!!res.hasNextPage);
      if (res.aggregations?.contentType && res.aggregations.contentType.length > 0) {
        setCategoryAggs(res.aggregations.contentType);
      }
      if (res.aggregations?.torrentTag && res.aggregations.torrentTag.length > 0) {
        setTagAggs(res.aggregations.torrentTag);
      }
    } catch (e: any) {
      setError(e.message || 'Error loading torrents');
    } finally {
      setLoading(false);
    }
  };

  createEffect(() => {
    fetchTorrents();
  });

  // Debounce search input: one query 300ms after the last keystroke.
  let debounceTimer: ReturnType<typeof setTimeout> | undefined;
  createEffect(() => {
    const q = queryString();
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      if (debouncedQuery() !== q) {
        setPage(1);
        setDebouncedQuery(q);
      }
    }, 300);
  });

  const toggleSort = (field: SortField) => {
    const cur = sort();
    if (cur.field === field) {
      setSort({ field, isAsc: !cur.isAsc });
    } else {
      const isAsc = field === 'published_at' || field === 'name' || field === 'info_hash';
      setSort({ field, isAsc });
    }
    setPage(1);
  };

  createEffect(() => {
    const q = debouncedQuery().trim();
    const p = page();
    const l = limit();
    const cats = selectedCategories();
    const tags = selectedTags();
    const s = sort();

    const params = new URLSearchParams();
    if (q) params.set('q', q);
    if (p > 1) params.set('page', p.toString());
    if (l !== 25) {
      const key = new URLSearchParams(window.location.search).has('limit') ? 'limit' : 'per_page';
      params.set(key, l.toString());
    }
    if (cats.length > 0) params.set('category', cats.join(','));
    if (tags.length > 0) params.set('tag', tags.join(','));
    if (s.field !== 'published_at' || !s.isAsc) {
      params.set('sort', `${s.isAsc ? '+' : '-'}${s.field}`);
    }

    const qs = params.toString();
    const newSearch = qs ? '?' + qs : '';
    if (window.location.search !== newSearch) {
      const newUrl = `${window.location.pathname}${newSearch}${window.location.hash}`;
      window.history.replaceState(null, '', newUrl);
    }
  });

  onMount(() => {
    const handlePopState = () => {
      clearTimeout(debounceTimer);
      const state = parseUrlState();
      setQueryString(state.q);
      setDebouncedQuery(state.q);
      setPage(state.page);
      setLimit(state.perPage);
      setSelectedCategories(state.categories);
      setSelectedTags(state.tags);
      setSort(state.sort);
    };
    window.addEventListener('popstate', handlePopState);
    onCleanup(() => {
      window.removeEventListener('popstate', handlePopState);
    });
  });

  const handleSearchSubmit = (e: Event) => {
    e.preventDefault();
    clearTimeout(debounceTimer);
    setPage(1);
    setDebouncedQuery(queryString());
  };

  const toggleCategory = (cat: string) => {
    setPage(1);
    if (selectedCategories().includes(cat)) {
      setSelectedCategories(selectedCategories().filter((c) => c !== cat));
    } else {
      setSelectedCategories([...selectedCategories(), cat]);
    }
  };

  const toggleTagFilter = (tag: string) => {
    setPage(1);
    if (selectedTags().includes(tag)) {
      setSelectedTags(selectedTags().filter((t) => t !== tag));
    } else {
      setSelectedTags([...selectedTags(), tag]);
    }
  };

  const toggleSelectAll = () => {
    const pageHashes = items().map((i) => i.torrent.infoHash);
    const allSelected = pageHashes.every((h) => selectedHashes().includes(h));
    if (allSelected) {
      setSelectedHashes(selectedHashes().filter((h) => !pageHashes.includes(h)));
    } else {
      const combined = new Set([...selectedHashes(), ...pageHashes]);
      setSelectedHashes(Array.from(combined));
    }
  };

  const deselectAll = () => {
    setSelectedHashes([]);
  };

  const toggleSelectRow = (hash: string) => {
    if (selectedHashes().includes(hash)) {
      setSelectedHashes(selectedHashes().filter((h) => h !== hash));
    } else {
      setSelectedHashes([...selectedHashes(), hash]);
    }
  };

  // Tag Management
  const openTagModal = (mode: 'add' | 'set' | 'remove', hashes: string[]) => {
    setTagModal({ open: true, mode, hashes, tags: [] });
  };

  const submitTagModal = async () => {
    const { mode, hashes, tags } = tagModal();
    if (hashes.length === 0) return;
    if (mode !== 'set' && tags.length === 0) return;

    try {
      if (mode === 'add') {
        await putTorrentTags(hashes, tags);
      } else if (mode === 'set') {
        await setTorrentTags(hashes, tags);
      } else if (mode === 'remove') {
        await deleteTorrentTags(hashes, tags);
      }
      setTagModal({ open: false, mode: 'add', hashes: [], tags: [] });
      fetchTorrents();
    } catch (err: any) {
      alert(`Tag operation failed: ${err.message}`);
    }
  };

  // Delete
  const openDeleteModal = (hashes: string[]) => {
    setDeleteModal({ open: true, hashes });
  };

  const confirmDelete = async () => {
    const hashes = deleteModal().hashes;
    if (hashes.length === 0) return;
    try {
      await deleteTorrents(hashes);
      setDeleteModal({ open: false, hashes: [] });
      setSelectedHashes(selectedHashes().filter((h) => !hashes.includes(h)));
      fetchTorrents();
    } catch (err: any) {
      alert(`Deletion failed: ${err.message}`);
    }
  };

  // Archive
  const openArchiveModal = (hashes: string[]) => {
    setArchiveModal({ open: true, hashes });
  };

  const confirmArchive = async () => {
    const hashes = archiveModal().hashes;
    if (hashes.length === 0) return;
    try {
      await archiveTorrents(hashes);
      setArchiveModal({ open: false, hashes: [] });
      setSelectedHashes(selectedHashes().filter((h) => !hashes.includes(h)));
      fetchTorrents();
    } catch (err: any) {
      alert(`Archival failed: ${err.message}`);
    }
  };

  const totalPages = () => Math.max(page(), Math.ceil(totalCount() / limit()) || 1);

  return (
    <div style={{ display: 'flex', 'flex-direction': 'column', flex: '1', 'min-height': '0' }}>
      {/* Search Header */}
      <form class="search-form" onSubmit={handleSearchSubmit}>
        <div style={{ position: 'relative', flex: 1 }}>
          <input
            type="text"
            placeholder="Search torrents by title, keyword, or hash..."
            value={queryString()}
            onInput={(e) => setQueryString(e.currentTarget.value)}
            style={{ margin: 0, 'padding-left': '2.25rem' }}
          />
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style={{ position: 'absolute', left: '0.75rem', top: '50%', transform: 'translateY(-50%)', color: 'var(--app-text-muted)' }}>
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
        </div>
        <button
          type="button"
          class="outline secondary mobile-filter-btn"
          onClick={() => setMobileFilterOpen(!mobileFilterOpen())}
          style={{ margin: 0, padding: '0 0.85rem', display: 'flex', 'align-items': 'center', gap: '0.4rem' }}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="22 3 2 3 10 12.46 10 19 14 21 14 12.46 22 3" />
          </svg>
          <span class="mobile-filter-text">Filters</span>
        </button>
      </form>

      {/* Batch Action Bar */}
      <Show when={selectedHashes().length > 0}>
        <div style={{ padding: '0.6rem 1rem', 'margin-bottom': '1rem', display: 'flex', 'align-items': 'center', 'justify-content': 'space-between', background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', 'flex-wrap': 'wrap', gap: '0.5rem' }}>
          <div style={{ display: 'flex', 'align-items': 'center', gap: '0.5rem' }}>
            <span style={{ 'font-size': '0.85rem' }}>
              <strong>{selectedHashes().length}</strong> item(s) selected
            </span>
            <button class="nav-btn" onClick={deselectAll}>
              Deselect all
            </button>
          </div>
          <div style={{ display: 'flex', gap: '0.4rem', 'flex-wrap': 'wrap' }}>
            <button class="nav-btn" onClick={() => openTagModal('add', selectedHashes())}>
              + Add Tags
            </button>
            <button class="nav-btn" onClick={() => openTagModal('set', selectedHashes())}>
              Set Tags
            </button>
            <button class="nav-btn" onClick={() => openTagModal('remove', selectedHashes())}>
              - Remove Tags
            </button>
            <button class="nav-btn" onClick={() => openArchiveModal(selectedHashes())}>
              Archive Selected
            </button>
            <button class="nav-btn danger" onClick={() => openDeleteModal(selectedHashes())} style={{ color: '#dc2626' }}>
              Delete Selected
            </button>
          </div>
        </div>
      </Show>

      {/* Mobile Backdrop (rendered outside the grid: as a grid child it would occupy the sidebar column and shift the layout) */}
      <Show when={mobileFilterOpen()}>
        <div class="mobile-filter-backdrop" onClick={() => setMobileFilterOpen(false)} />
      </Show>

      {/* Main Layout Grid */}
      <div class="grid-container" style={{ display: 'grid', 'grid-template-columns': '140px 1fr', 'grid-template-rows': 'minmax(0, 1fr)', gap: '1.25rem', flex: '1', 'min-height': '0' }}>
        {/* Sidebar Filter Pane */}
        <aside class={`desktop-sidebar ${mobileFilterOpen() ? 'mobile-open' : ''}`}>
          <Show when={mobileFilterOpen()}>
            <div style={{ display: 'flex', 'justify-content': 'flex-end', 'margin-bottom': '0.5rem' }}>
              <button class="btn-icon" onClick={() => setMobileFilterOpen(false)}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6 6 18M6 6l12 12" /></svg>
              </button>
            </div>
          </Show>

          <div class="mobile-only-section" style={{ 'margin-bottom': '1rem' }}>
            <div style={{ 'font-weight': '600', 'font-size': '0.85rem', 'margin-bottom': '0.45rem' }}>Columns</div>
            <div style={{ display: 'flex', 'flex-wrap': 'wrap', gap: '0.35rem' }}>
              <For each={TOGGLABLE_COLUMNS}>
                {(col) => {
                  const active = () => mobileVisibleCols().has(col.key);
                  return (
                    <button
                      type="button"
                      class={`nav-btn btn-sm ${active() ? 'active' : ''}`}
                      onClick={() => toggleCol(col.key)}
                      style={{ cursor: 'pointer' }}
                    >
                      {col.label}
                    </button>
                  );
                }}
              </For>
            </div>
          </div>

          <div style={{ 'margin-bottom': '1rem' }}>
            <div style={{ 'font-weight': '600', 'font-size': '0.85rem' }}>Categories</div>
            <div style={{ 'padding-top': '0.4rem' }}>
              <For each={categories}>
                {(cat) => {
                  return (
                    <label style={{ display: 'flex', 'align-items': 'center', 'justify-content': 'space-between', gap: '0.4rem', margin: '0.3rem 0', cursor: 'pointer', 'font-size': '0.82rem' }}>
                      <span style={{ display: 'flex', 'align-items': 'center', gap: '0.4rem' }}>
                        <input
                          type="checkbox"
                          checked={selectedCategories().includes(cat.value)}
                          onChange={() => toggleCategory(cat.value)}
                          style={{ margin: 0 }}
                        />
                        {cat.label}
                      </span>
                      <span class="badge">{categoryAggs().find((a) => a.value === cat.value)?.count ?? 0}</span>
                    </label>
                  );
                }}
              </For>
            </div>
          </div>

          <div style={{ display: 'flex', 'flex-direction': 'column', flex: '1', 'min-height': '0' }}>
            <div style={{ 'font-weight': '600', 'font-size': '0.85rem' }}>Tags</div>
            <div style={{ 'padding-top': '0.4rem', flex: '1', 'min-height': '0', 'overflow-y': 'auto' }}>
              <Show when={tagAggs().length === 0}>
                <small style={{ color: 'var(--app-text-muted)' }}>No tags available</small>
              </Show>
              <For each={tagAggs()}>
                {(tagAgg) => (
                  <label style={{ display: 'flex', 'align-items': 'center', 'justify-content': 'space-between', gap: '0.4rem', margin: '0.3rem 0', cursor: 'pointer', 'font-size': '0.82rem' }}>
                    <span style={{ display: 'flex', 'align-items': 'center', gap: '0.4rem' }}>
                      <input
                        type="checkbox"
                        checked={selectedTags().includes(tagAgg.value)}
                        onChange={() => toggleTagFilter(tagAgg.value)}
                        style={{ margin: 0 }}
                      />
                      {tagAgg.label || tagAgg.value}
                    </span>
                    <span class="badge">{tagAgg.count}</span>
                  </label>
                )}
              </For>
            </div>
          </div>
        </aside>

        {/* Table Content */}
        <main style={{ margin: 0, 'overflow-y': 'auto', 'min-height': '0' }}>
          <Show when={error()}>
            <div style={{ background: '#fef2f2', border: '1px solid #fca5a5', color: '#dc2626', padding: '0.75rem', 'border-radius': 'var(--app-radius)', 'margin-bottom': '1rem', 'font-size': '0.85rem' }}>
              {error()}
            </div>
          </Show>

          <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-bottom': '0.4rem', 'font-size': '0.82rem', color: 'var(--app-text-muted)' }}>
            <span>
              Showing {items().length} of <strong style={{ color: 'var(--app-text)' }}>{totalCount()}{totalCountIsEstimate() ? '+' : ''}</strong> torrents
              <span class="list-spinner" style={{ visibility: loading() ? 'visible' : 'hidden' }} />
            </span>
            <div style={{ display: 'flex', 'align-items': 'center', gap: '0.4rem', 'flex-shrink': 0 }}>
              <label style={{ margin: 0, display: 'flex', 'align-items': 'center', gap: '0.4rem', 'white-space': 'nowrap' }}>
                Per page:
                <select
                  value={limit()}
                  onChange={(e) => {
                    setLimit(parseInt(e.currentTarget.value, 10));
                    setPage(1);
                  }}
                  style={{ margin: 0, padding: '0.15rem 2rem 0.15rem 0.4rem', 'font-size': '0.82rem' }}
                >
                  <option value={10}>10</option>
                  <option value={25}>25</option>
                  <option value={50}>50</option>
                  <option value={100}>100</option>
                </select>
              </label>
            </div>
          </div>

          <div class="list-sort-bar" style={{ display: 'flex', 'align-items': 'center', gap: '0.35rem', 'margin-bottom': '0.4rem', 'font-size': '0.78rem', color: 'var(--app-text-muted)', 'flex-wrap': 'wrap' }}>
            <For each={SORT_OPTIONS}>
              {(opt) => (
                <button
                  class={`nav-btn btn-sm ${sort().field === opt.field ? 'active' : ''}`}
                  onClick={() => toggleSort(opt.field)}
                >
                  {opt.label}
                  {sort().field === opt.field ? (sort().isAsc ? ' ▲' : ' ▼') : ''}
                </button>
              )}
            </For>
          </div>

          <div style={{ 'overflow-x': 'auto' }}>
            <table class="compact-table" style={{ 'margin-bottom': 0 }}>
              <thead>
                <tr>
                  <th class="col-select" style={{ width: '36px', 'text-align': 'center' }}>
                    <input
                      type="checkbox"
                      checked={items().length > 0 && items().every((i) => selectedHashes().includes(i.torrent.infoHash))}
                      onChange={toggleSelectAll}
                      style={{ margin: 0 }}
                    />
                  </th>
                  <th class="torrent-title-col">Name / Content</th>
                  <th class={`col-fit ${!mobileVisibleCols().has('size') ? 'col-hide-mobile' : ''}`}>Size</th>
                  <th class={`col-fit ${!mobileVisibleCols().has('peers') ? 'col-hide-mobile' : ''}`}>Seeds / Leech</th>
                  <th class={`col-fit ${!mobileVisibleCols().has('category') ? 'col-hide-mobile' : ''}`}>Category</th>
                  <th class={`col-fit col-tags ${!mobileVisibleCols().has('tags') ? 'col-hide-mobile' : ''}`}>Tags</th>
                  <th class={`col-magnet ${!mobileVisibleCols().has('magnet') ? 'col-hide-mobile' : ''}`} style={{ width: '44px' }}></th>
                </tr>
              </thead>
              <tbody>
                <Show when={!loading() && items().length === 0}>
                  <tr>
                    <td colspan="7" style={{ 'text-align': 'center', padding: '2rem', color: 'var(--app-text-muted)' }}>
                      No torrents found matching criteria.
                    </td>
                  </tr>
                </Show>
                <For each={items()}>
                  {(item) => {
                    const hash = item.torrent.infoHash;
                    const isSelected = () => selectedHashes().includes(hash);
                    const open = () => expanded() === hash;

                    let touchTimer: ReturnType<typeof setTimeout> | null = null;
                    let touchStartX = 0;
                    let touchStartY = 0;
                    let didLongPress = false;

                    const onTouchStart = (e: TouchEvent) => {
                      didLongPress = false;
                      if (e.touches.length !== 1) return;
                      const touch = e.touches[0];
                      touchStartX = touch.clientX;
                      touchStartY = touch.clientY;
                      touchTimer = setTimeout(() => {
                        didLongPress = true;
                        toggleSelectRow(hash);
                        if ('vibrate' in navigator) {
                          try { navigator.vibrate(50); } catch (_) { }
                        }
                      }, 500);
                    };

                    const onTouchMove = (e: TouchEvent) => {
                      if (!touchTimer) return;
                      const touch = e.touches[0];
                      if (Math.hypot(touch.clientX - touchStartX, touch.clientY - touchStartY) > 10) {
                        clearTimeout(touchTimer);
                        touchTimer = null;
                      }
                    };

                    const onTouchEnd = () => {
                      if (touchTimer) {
                        clearTimeout(touchTimer);
                        touchTimer = null;
                      }
                    };

                    const onTouchCancel = () => {
                      if (touchTimer) {
                        clearTimeout(touchTimer);
                        touchTimer = null;
                      }
                    };

                    const onRowClick = (e: MouseEvent) => {
                      if (didLongPress) {
                        didLongPress = false;
                        return;
                      }
                      if ((e.target as HTMLElement).closest('button, a, input, select, textarea, .tag-editor, .hash-copy')) return;
                      setExpanded(open() ? null : hash);
                    };

                    const onContextMenu = (e: MouseEvent) => {
                      if (didLongPress) {
                        e.preventDefault();
                      }
                    };

                    return (
                      <>
                        <tr
                          class={isSelected() ? 'selected-row' : ''}
                          onClick={onRowClick}
                          onContextMenu={onContextMenu}
                          onTouchStart={onTouchStart}
                          onTouchMove={onTouchMove}
                          onTouchEnd={onTouchEnd}
                          onTouchCancel={onTouchCancel}
                          style={{ cursor: 'pointer' }}
                        >
                          <td class="col-select" style={{ 'text-align': 'center' }}>
                            <input
                              type="checkbox"
                              checked={isSelected()}
                              onChange={() => toggleSelectRow(hash)}
                              style={{ margin: 0 }}
                            />
                          </td>
                          <td class="torrent-title-col">
                            <div style={{ display: 'flex', 'align-items': 'flex-start', gap: '0.35rem', 'min-width': 0 }}>
                              <span
                                class="row-expand-icon"
                                style={{
                                  display: 'inline-block',
                                  'font-size': '0.65rem',
                                  color: 'var(--app-text-muted)',
                                  'margin-top': '0.25rem',
                                  transform: open() ? 'rotate(90deg)' : 'none',
                                  transition: 'transform 0.15s',
                                  'flex-shrink': '0',
                                }}
                              >
                                ▶
                              </span>
                              <div style={{ 'min-width': 0, flex: '1' }}>
                                <div class="torrent-title-text" title={item.title || item.torrent.name}>
                                  {item.title || item.torrent.name}
                                </div>
                                <div style={{ display: 'flex', 'align-items': 'center', gap: '0.4rem', 'flex-wrap': 'wrap', 'margin-top': '0.15rem' }}>
                                  <span
                                    class="hash-copy"
                                    title="Click to copy the full hash"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      copyHash(hash);
                                    }}
                                  >
                                    {copiedHash() === hash ? 'Copied to clipboard!' : hash}
                                  </span>
                                  <div class="desktop-title-tags" style={{ display: 'inline-flex', 'flex-wrap': 'wrap', gap: '0.2rem' }}>
                                    <For each={tagsFor(item)}>
                                      {(tag) => (
                                        <span class="badge">
                                          {tag}
                                        </span>
                                      )}
                                    </For>
                                  </div>
                                </div>
                                <Show when={item.content?.overview}>
                                  <div style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'margin-top': '0.2rem', display: '-webkit-box', '-webkit-line-clamp': 2, '-webkit-box-orient': 'vertical', overflow: 'hidden' }}>
                                    {item.content?.overview}
                                  </div>
                                </Show>
                              </div>
                            </div>
                          </td>
                          <td class={`col-fit ${!mobileVisibleCols().has('size') ? 'col-hide-mobile' : ''}`} style={{ 'white-space': 'nowrap' }}>
                            <div title={String(item.torrent.size)}>{formatSize(item.torrent.size)}</div>
                            <Show when={item.torrent.filesStatus === 'single' || item.torrent.filesCount != null}>
                              <div style={{ color: 'var(--app-text-muted)', 'font-size': '0.72rem', 'margin-top': '0.1rem' }}>
                                {item.torrent.filesStatus === 'single'
                                  ? 'single'
                                  : `${item.torrent.filesCount} ${item.torrent.filesCount === 1 ? 'file' : 'files'}`}
                              </div>
                            </Show>
                          </td>
                          <td class={`col-fit ${!mobileVisibleCols().has('peers') ? 'col-hide-mobile' : ''}`} style={{ 'white-space': 'nowrap', 'font-size': '0.8rem' }}>
                            <span style={{ color: '#16a34a', 'font-weight': '500' }}>▲ {item.torrent.seeders || item.seeders || 0}</span>
                            <span style={{ color: 'var(--app-text-muted)', margin: '0 0.2rem' }}>/</span>
                            <span style={{ color: '#dc2626', 'font-weight': '500' }}>▼ {item.torrent.leechers || item.leechers || 0}</span>
                          </td>
                          <td class={`col-fit ${!mobileVisibleCols().has('category') ? 'col-hide-mobile' : ''}`}>
                            <span style={{ 'text-transform': 'capitalize', 'font-size': '0.8rem' }}>
                              {item.contentType || 'unclassified'}
                            </span>
                            <Show when={item.publishedAt}>
                              <div
                                title={new Date(item.publishedAt!).toLocaleString()}
                                style={{ color: 'var(--app-text-muted)', 'font-size': '0.72rem', 'margin-top': '0.1rem', 'white-space': 'nowrap' }}
                              >
                                {formatAge(item.publishedAt!)}
                              </div>
                            </Show>
                          </td>
                          <td class={`col-fit col-tags ${!mobileVisibleCols().has('tags') ? 'col-hide-mobile' : ''}`}>
                            <div style={{ display: 'flex', 'flex-wrap': 'wrap', gap: '0.2rem' }}>
                              <For each={tagsFor(item)}>
                                {(tag) => (
                                  <span class="badge">
                                    {tag}
                                  </span>
                                )}
                              </For>
                            </div>
                          </td>
                          <td class={`col-magnet ${!mobileVisibleCols().has('magnet') ? 'col-hide-mobile' : ''}`} style={{ 'text-align': 'center' }}>
                            <Show when={item.torrent.magnetUri}>
                              <a
                                href={item.torrent.magnetUri}
                                title="Copy magnet"
                                class="btn-icon"
                                style={{ color: '#334155' }}
                                onClick={(e) => e.stopPropagation()}
                              >
                                <MagnetIcon size={20} />
                              </a>
                            </Show>
                          </td>
                        </tr>
                        <tr>
                          <td colspan={7} style={{ padding: 0, border: 'none', background: '#f8fafc' }}>
                            <Disclosure open={open()} onBeforeMatch={() => setExpanded(hash)}>
                              <FilesPanel
                                hash={hash}
                                active={open()}
                                isSingleFile={item.torrent.filesStatus === 'single' || item.torrent.filesCount === 1}
                                tagsSlot={
                                  <TagEditor
                                    tags={tagsFor(item)}
                                    onSave={(t) => saveTags(hash, t)}
                                  />
                                }
                              />
                            </Disclosure>
                          </td>
                        </tr>
                      </>
                    );
                  }}
                </For>
              </tbody>
            </table>
          </div>

          {/* Pagination Controls */}
          <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-top': '0.35rem' }}>
            <button
              class="nav-btn"
              disabled={page() <= 1}
              onClick={() => setPage(page() - 1)}
            >
              ← Previous
            </button>
            <span style={{ 'font-size': '0.82rem', color: 'var(--app-text-muted)' }}>
              Page <strong style={{ color: 'var(--app-text)' }}>{page()}</strong> of <strong style={{ color: 'var(--app-text)' }}>{totalPages()}{totalCountIsEstimate() ? '+' : ''}</strong>
            </span>
            <button
              class="nav-btn"
              disabled={!hasNextPage() && page() >= totalPages()}
              onClick={() => setPage(page() + 1)}
            >
              Next →
            </button>
          </div>
        </main>
      </div>

      {/* Tag Management Modal */}
      <Show when={tagModal().open}>
        <dialog open>
          <article style={{ 'max-width': '480px', width: '100%', padding: '1.25rem' }}>
            <header style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center' }}>
              <h5 style={{ margin: 0, 'font-size': '1rem', 'text-transform': 'capitalize' }}>
                {tagModal().mode} Tags ({tagModal().hashes.length} selected)
              </h5>
              <button class="btn-icon" onClick={() => setTagModal({ ...tagModal(), open: false })}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6 6 18M6 6l12 12" /></svg>
              </button>
            </header>

            <div style={{ margin: '1rem 0' }}>
              <TagEditor
                tags={tagModal().tags}
                onSave={(t) => setTagModal({ ...tagModal(), tags: t })}
                style={{ padding: '0' }}
              />
            </div>

            <footer style={{ display: 'flex', 'justify-content': 'flex-end', gap: '0.5rem' }}>
              <button class="nav-btn" onClick={() => setTagModal({ ...tagModal(), open: false })}>
                Cancel
              </button>
              <button class="nav-btn active" onClick={submitTagModal}>
                Apply
              </button>
            </footer>
          </article>
        </dialog>
      </Show>

      {/* Delete Modal */}
      <Show when={deleteModal().open}>
        <dialog open>
          <article style={{ 'max-width': '420px', width: '100%', padding: '1.25rem' }}>
            <header>
              <h5 style={{ margin: 0, 'font-size': '1rem' }}>Confirm Deletion</h5>
            </header>
            <p style={{ 'font-size': '0.88rem', margin: '0.75rem 0' }}>
              Are you sure you want to delete <strong>{deleteModal().hashes.length}</strong> torrent(s)? This action cannot be undone.
            </p>
            <footer style={{ display: 'flex', 'justify-content': 'flex-end', gap: '0.5rem' }}>
              <button class="nav-btn" onClick={() => setDeleteModal({ open: false, hashes: [] })}>
                Cancel
              </button>
              <button class="nav-btn danger" onClick={confirmDelete} style={{ background: '#dc2626', color: '#ffffff' }}>
                Delete Permanently
              </button>
            </footer>
          </article>
        </dialog>
      </Show>

      {/* Archive Modal */}
      <Show when={archiveModal().open}>
        <dialog open>
          <article style={{ 'max-width': '420px', width: '100%', padding: '1.25rem' }}>
            <header>
              <h5 style={{ margin: 0, 'font-size': '1rem' }}>Confirm Archival</h5>
            </header>
            <p style={{ 'font-size': '0.88rem', margin: '0.75rem 0' }}>
              Are you sure you want to archive <strong>{archiveModal().hashes.length}</strong> torrent(s)? Metadata will be moved to cold storage.
            </p>
            <footer style={{ display: 'flex', 'justify-content': 'flex-end', gap: '0.5rem' }}>
              <button class="nav-btn" onClick={() => setArchiveModal({ open: false, hashes: [] })}>
                Cancel
              </button>
              <button class="nav-btn active" onClick={confirmArchive}>
                Archive
              </button>
            </footer>
          </article>
        </dialog>
      </Show>
    </div>
  );
};
