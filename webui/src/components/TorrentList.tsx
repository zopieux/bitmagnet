import { createSignal, createEffect, For, Show, Component } from 'solid-js';
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

function formatSize(bytes: number): string {  if (!bytes || bytes === 0) return '0 B';
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
const FilesPanel: Component<{ hash: string; active: boolean }> = (props) => {
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
    sortField() === field ? (sortDesc() ? ' ↓' : ' ↑') : '';

  return (
    <>
      <div
        class="cell-controls"
        style={{ display: 'flex', 'align-items': 'center', gap: '0.5rem', padding: '0.75rem 0 0 1rem' }}
        onClick={(e) => e.stopPropagation()}
      >
        <button
          class={`nav-btn ${sortField() === 'path' ? 'active' : ''}`}
          style={{ height: '2.2rem', display: 'inline-flex', 'align-items': 'center' }}
          onClick={() => toggleSort('path')}
        >
          Filename{sortMark('path')}
        </button>
        <button
          class={`nav-btn ${sortField() === 'size' ? 'active' : ''}`}
          style={{ height: '2.2rem', display: 'inline-flex', 'align-items': 'center' }}
          onClick={() => toggleSort('size')}
        >
          Size{sortMark('size')}
        </button>
        <input
          type="text"
          placeholder="Filter by filename..."
          value={searchText()}
          onInput={(e) => onSearchInput(e.currentTarget.value)}
          onClick={(e) => e.stopPropagation()}
          style={{ margin: 0, padding: '0.3rem 0.6rem', 'font-size': '0.82rem', flex: '1', 'min-width': '8rem', height: '2.2rem' }}
        />
      </div>
      <div class="cell-table" style={{ padding: '0.5rem 1rem 0.75rem' }}>
        <Show when={loading() && !data()}>
          <div style={{ 'font-size': '0.82rem', color: 'var(--app-text-muted)' }}>Loading files...</div>
        </Show>
        <Show when={data() && data()!.totalCount === 0}>
          <div style={{ 'font-size': '0.82rem', color: 'var(--app-text-muted)' }}>
            {appliedQuery() ? `No files match "${appliedQuery()}".` : 'No file information available for this torrent.'}
          </div>
        </Show>
        <Show when={data() && data()!.totalCount > 0}>
          <table class="compact-table" style={{ margin: 0, 'border-radius': 0 }}>
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
          <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-top': '0.6rem' }}>
            <span style={{ 'font-size': '0.78rem', color: 'var(--app-text-muted)' }}>
              {totalPages() > 1 ? `${(page() - 1) * PAGE_SIZE + 1}–${Math.min(page() * PAGE_SIZE, data()!.totalCount)} of ` : ''}
              {data()!.totalCount} files
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
      style={{ display: 'flex', 'align-items': 'center', gap: '0.35rem', padding: '0.75rem 1rem 0', ...props.style }}
    >
      <span style={{ 'font-size': '0.78rem', 'font-weight': '600', color: 'var(--app-text-muted)', 'margin-right': '0.25rem' }}>Tags:</span>
      <For each={props.tags}>
        {(tag, idx) => (
          <Show
            when={editingIdx() === idx()}
            fallback={
              <span
                class="badge tag-chip"
                data-idx={idx()}
                tabIndex={-1}
                style={{ 'flex-shrink': 0 }}
                onClick={() => focusChip(idx())}
                onDblClick={() => startEdit(idx())}
              >
                {tag}
              </span>
            }
          >
            <input
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
              style={{ margin: 0, padding: '0.1rem 0.4rem', 'font-size': '0.75rem', width: '9rem', height: '2.2rem' }}
            />
          </Show>
        )}
      </For>
      <input
        type="text"
        placeholder={props.tags.length === 0 ? 'Add a tag, Space or Enter to save...' : 'Add tag...'}
        value={newText()}
        ref={(el) => (inputRef = el)}
        onInput={(e) => setNewText(e.currentTarget.value)}
        onClick={(e) => e.stopPropagation()}
        style={{ margin: 0, padding: '0.2rem 0.5rem', 'font-size': '0.78rem', flex: '1 1 auto', 'min-width': '10rem', height: '2.2rem' }}
      />
    </div>
  );
};

export const TorrentList: Component = () => {
  const [queryString, setQueryString] = createSignal('');
  const [debouncedQuery, setDebouncedQuery] = createSignal('');
  const [sort, setSort] = createSignal<{ field: string; descending: boolean } | null>(null);
  const [selectedCategories, setSelectedCategories] = createSignal<string[]>([]);
  const [selectedTags, setSelectedTags] = createSignal<string[]>([]);
  const [page, setPage] = createSignal(1);
  const [limit, setLimit] = createSignal(25);

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

  // Mobile Filter Pane State
  const [mobileFilterOpen, setMobileFilterOpen] = createSignal(false);

  // Row expansion (actions, tag editor, files), keyed by info hash
  const [expanded, setExpanded] = createSignal<string | null>(null);
  const [copiedHash, setCopiedHash] = createSignal<string | null>(null);
  const copyHash = (hash: string) => {
    navigator.clipboard?.writeText(hash).catch(() => {});
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
      const res = await searchTorrents({
        queryString: debouncedQuery(),
        page: page(),
        limit: limit(),
        contentTypes: selectedCategories(),
        tags: selectedTags(),
        orderBy: sort() ?? undefined,
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

  const toggleSort = (field: string) => {
    const cur = sort();
    if (cur && cur.field === field) {
      setSort({ field, descending: !cur.descending });
    } else {
      // text fields sort ascending first, numeric fields descending
      setSort({ field, descending: field !== 'name' && field !== 'info_hash' });
    }
    setPage(1);
  };

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
      <form onSubmit={handleSearchSubmit} style={{ display: 'flex', gap: '0.75rem', 'margin-bottom': '1.25rem', flex: 'none' }}>
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
      <div class="grid-container" style={{ display: 'grid', 'grid-template-columns': '220px 1fr', 'grid-template-rows': 'minmax(0, 1fr)', gap: '1.25rem', flex: '1', 'min-height': '0' }}>
        {/* Sidebar Filter Pane */}
        <aside class={`desktop-sidebar ${mobileFilterOpen() ? 'mobile-open' : ''}`}>
          <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-bottom': '0.75rem' }}>
            <span style={{ 'font-weight': '600', 'font-size': '0.85rem', 'text-transform': 'uppercase', color: 'var(--app-text-muted)' }}>Filters</span>
            <Show when={mobileFilterOpen()}>
              <button class="btn-icon" onClick={() => setMobileFilterOpen(false)}>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6 6 18M6 6l12 12"/></svg>
              </button>
            </Show>
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

          <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-bottom': '0.75rem', 'font-size': '0.82rem', color: 'var(--app-text-muted)' }}>
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

          <div style={{ display: 'flex', 'align-items': 'center', gap: '0.35rem', 'margin-bottom': '0.6rem', 'font-size': '0.78rem', color: 'var(--app-text-muted)', 'flex-wrap': 'wrap' }}>
            <span>Sort:</span>
            <For
              each={[
                { field: 'name', label: 'Name' },
                { field: 'info_hash', label: 'Hash' },
                { field: 'published_at', label: 'Age' },
                { field: 'seeders', label: 'Seeds' },
                { field: 'leechers', label: 'Leech' },
                { field: 'size', label: 'Size' },
                { field: 'files_count', label: 'Files' },
              ]}
            >
              {(opt) => (
                <button
                  class={`nav-btn ${sort()?.field === opt.field ? 'active' : ''}`}
                  style={{ padding: '0.15rem 0.5rem', 'font-size': '0.75rem' }}
                  onClick={() => toggleSort(opt.field)}
                >
                  {opt.label}
                  {sort()?.field === opt.field ? (sort()!.descending ? ' ▼' : ' ▲') : ''}
                </button>
              )}
            </For>
          </div>

          <div style={{ 'overflow-x': 'auto' }}>
            <table class="compact-table">
              <thead>
                <tr>
                  <th style={{ width: '36px', 'text-align': 'center' }}>
                    <input
                      type="checkbox"
                      checked={items().length > 0 && items().every((i) => selectedHashes().includes(i.torrent.infoHash))}
                      onChange={toggleSelectAll}
                      style={{ margin: 0 }}
                    />
                  </th>
                  <th class="torrent-title-col">Name / Content</th>
                  <th class="col-fit">Category</th>
                  <th class="col-fit">Size</th>
                  <th class="col-fit">Seeds / Leech</th>
                  <th class="col-fit">Tags</th>
                  <th style={{ width: '44px' }}></th>
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
                    const onRowClick = (e: MouseEvent) => {
                      if ((e.target as HTMLElement).closest('button, a, input, select, textarea, .tag-editor, .hash-copy')) return;
                      setExpanded(open() ? null : hash);
                    };
                    return (
                      <>
                        <tr onClick={onRowClick} style={{ cursor: 'pointer' }}>
                          <td style={{ 'text-align': 'center' }}>
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
                                <div
                                  class="hash-copy"
                                  title="Click to copy the full hash"
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    copyHash(hash);
                                  }}
                                  style={{ color: 'var(--app-text-muted)', 'font-size': '0.72rem', 'margin-top': '0.1rem', 'font-family': 'monospace', cursor: 'pointer', 'word-break': 'break-all' }}
                                >
                                  {copiedHash() === hash ? 'Copied to clipboard!' : hash}
                                </div>
                                <Show when={item.content?.overview}>
                                  <div style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'margin-top': '0.2rem', display: '-webkit-box', '-webkit-line-clamp': 2, '-webkit-box-orient': 'vertical', overflow: 'hidden' }}>
                                    {item.content?.overview}
                                  </div>
                                </Show>
                              </div>
                            </div>
                          </td>
                          <td class="col-fit">
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
                          <td class="col-fit" style={{ 'white-space': 'nowrap' }}>
                            <div title={String(item.torrent.size)}>{formatSize(item.torrent.size)}</div>
                            <Show when={item.torrent.filesStatus === 'single' || item.torrent.filesCount != null}>
                              <div style={{ color: 'var(--app-text-muted)', 'font-size': '0.72rem', 'margin-top': '0.1rem' }}>
                                {item.torrent.filesStatus === 'single'
                                  ? 'single'
                                  : `${item.torrent.filesCount} ${item.torrent.filesCount === 1 ? 'file' : 'files'}`}
                              </div>
                            </Show>
                          </td>
                          <td class="col-fit" style={{ 'white-space': 'nowrap', 'font-size': '0.8rem' }}>
                            <span style={{ color: '#16a34a', 'font-weight': '500' }}>▲ {item.torrent.seeders || item.seeders || 0}</span>
                            <span style={{ color: 'var(--app-text-muted)', margin: '0 0.2rem' }}>/</span>
                            <span style={{ color: '#dc2626', 'font-weight': '500' }}>▼ {item.torrent.leechers || item.leechers || 0}</span>
                          </td>
                          <td class="col-fit">
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
                          <td style={{ 'text-align': 'center' }}>
                            <Show when={item.torrent.magnetUri}>
                              <a
                                href={item.torrent.magnetUri}
                                title="Download Magnet"
                                class="btn-icon"
                                style={{ color: '#334155' }}
                                onClick={(e) => e.stopPropagation()}
                              >
                                <svg width="20" height="20" viewBox="0 0 256 256" fill="currentColor" aria-hidden="true">
                                  <path d="M204.10547,53.05469A83.43674,83.43674,0,0,0,144.57227,28.001C144.46582,28,144.35938,28,144.252,28A83.43215,83.43215,0,0,0,84.8457,52.62012L18.74219,119.53223a12.0281,12.0281,0,0,0,.05273,16.91894l28.65332,28.65332a11.914,11.914,0,0,0,8.48438,3.51563h.07129a11.91554,11.91554,0,0,0,8.51367-3.61426l65.61523-67.16309a20.09518,20.09518,0,0,1,27.99219-.25683,19.55435,19.55435,0,0,1,5.95117,14.03906,20.87149,20.87149,0,0,1-6.01269,14.85645l-67.0166,64.99218a11.9984,11.9984,0,0,0-.13086,17.09864l28.65429,28.65429a12.03749,12.03749,0,0,0,16.88672.082l32.93262-32.2951.001-.00079.00048-.0008,33.55518-32.90545C235.84766,139.20605,236.36719,85.7998,204.10547,53.05469ZM58.79492,159.416a3.97331,3.97331,0,0,1-2.83691,1.2041h-.02442a3.9766,3.9766,0,0,1-2.8291-1.17188L24.45117,130.79492a4.00968,4.00968,0,0,1-.01758-5.63965L53.97412,95.25262l34.146,34.146Zm72.06055,72.18164a4.01382,4.01382,0,0,1-5.62891-.02735L96.57227,202.916a4.00027,4.00027,0,0,1,.04492-5.7002l30.11474-29.20563,34.146,34.14624Zm35.73437-35.04218-34.11523-34.11481,31.20215-30.26a28.9462,28.9462,0,0,0,8.39844-20.63183,27.49364,27.49364,0,0,0-8.40137-19.72656,28.1387,28.1387,0,0,0-39.21289.3789c-.01172.01074-.02246.02149-.03321.0332l-30.71728,31.442L59.59619,89.562,90.51953,58.25977A75.47294,75.47294,0,0,1,144.25293,36c.0957,0,.19434,0,.29.001a75.486,75.486,0,0,1,53.86328,22.66894c29.18945,29.625,28.68848,77.97559-1.08887,107.75293Z" />
                                </svg>
                              </a>
                            </Show>
                          </td>
                        </tr>
                        <tr>
                          <td colspan={7} style={{ padding: 0, border: 'none', background: '#f8fafc' }}>
                            <Disclosure open={open()} onBeforeMatch={() => setExpanded(hash)}>
                              <div class="expanded-grid">
                                <div class="cell-tags">
                                  <TagEditor tags={tagsFor(item)} onSave={(t) => saveTags(hash, t)} style={{ padding: '0.75rem 1rem 0 0' }} />
                                </div>
                                <FilesPanel hash={hash} active={open()} />
                              </div>
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
          <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-top': '1rem' }}>
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
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M18 6 6 18M6 6l12 12"/></svg>
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
