import { render } from 'solid-js/web';
import { createSignal } from 'solid-js';
import '@picocss/pico';
import { TorrentList } from './components/TorrentList';
import { AdminDashboard } from './components/AdminDashboard';
import { MagnetIcon } from './components/MagnetIcon';

const App = () => {
  const [tab, setTab] = createSignal<'torrents' | 'admin'>('torrents');

  return (
    <div class="app-wrapper">
      <style>{`
        :root {
          --font-family-sans: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
          --pico-font-family: var(--font-family-sans);
          --pico-font-size: 14px;
          --pico-border-radius: 4px;
          --pico-primary: #2563eb;
          --pico-primary-background: #2563eb;
          --pico-primary-hover: #1d4ed8;
          --app-bg: #f8fafc;
          --app-card-bg: #ffffff;
          --app-border: #e2e8f0;
          --app-text: #0f172a;
          --app-text-muted: #64748b;
          --app-radius: 6px;
          --app-gap-sm: 0.5rem;
          --app-gap-md: 1rem;
          --app-gap-lg: 1.5rem;
          --app-control-height-sm: 1.85rem;
        }

        html, body, #root {
          height: 100%;
        }

        body {
          font-family: var(--font-family-sans);
          background-color: var(--app-bg);
          color: var(--app-text);
          font-size: 14px;
          margin: 0;
          padding: 0;
        }

        /* App shell: the wrapper is exactly the viewport height and never
           scrolls; regions inside it (table column, tags list, dashboard)
           scroll internally. */
        .app-wrapper {
          height: 100dvh;
          display: flex;
          flex-direction: column;
        }

        .app-wrapper > main {
          flex: 1;
          min-height: 0;
          display: flex;
          flex-direction: column;
        }

        .dashboard-root {
          flex: 1;
          min-height: 0;
          overflow-y: auto;
        }

        .expanded-row-toolbar {
          display: flex;
          align-items: center;
          gap: 0.6rem;
          padding: 0.5rem 1rem;
          box-sizing: border-box;
        }

        .expanded-row-toolbar .search-sort-group {
          display: flex;
          align-items: center;
          gap: 0.4rem;
          flex: 0 0 60%;
          width: 60%;
          max-width: 60%;
          min-width: 0;
        }

        .expanded-row-toolbar .filename-filter-input {
          flex: 1 1 auto;
          min-width: 0;
        }

        .expanded-row-toolbar .tag-editor {
          flex: 0 0 40%;
          width: 40%;
          max-width: 40%;
          min-width: 0;
          display: flex;
          align-items: center;
          gap: 0.35rem;
          box-sizing: border-box;
          overflow-x: auto;
          scrollbar-width: none;
        }

        .expanded-row-toolbar .tag-editor::-webkit-scrollbar {
          display: none;
        }

        .expanded-row-toolbar.single-file .tag-editor {
          flex: 0 1 40%;
          width: 40%;
          max-width: 40%;
        }

        .app-wrapper {
          max-width: 1280px;
          margin: 0 auto;
          padding: 0.85rem 1.25rem 1.25rem 1.25rem;
        }

        header.app-header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          padding: 0.25rem 0;
          margin-bottom: 0.85rem;
        }

        header.app-header nav,
        header.app-header nav ul,
        header.app-header nav li {
          margin: 0;
          padding: 0;
          height: auto;
          min-height: 0;
        }

        .brand-logo {
          display: flex;
          align-items: center;
          gap: 0.45rem;
          font-weight: 700;
          font-size: 1.1rem;
          color: var(--app-text);
          text-decoration: none;
        }

        .nav-tabs {
          display: flex;
          gap: 0.35rem;
          margin: 0;
          padding: 0;
          list-style: none;
        }

        .nav-btn {
          padding: 0.35rem 0.75rem;
          font-size: 0.85rem;
          font-weight: 500;
          border-radius: var(--app-radius);
          border: 1px solid transparent;
          background: transparent;
          color: var(--app-text-muted);
          cursor: pointer;
          transition: all 0.15s ease;
          margin: 0;
        }

        .nav-btn.active {
          background: #ffffff;
          color: var(--pico-primary);
          border-color: var(--app-border);
          box-shadow: 0 1px 2px rgba(0,0,0,0.05);
        }

        .nav-btn:hover:not(.active) {
          background: #f1f5f9;
          color: var(--app-text);
        }

        .nav-btn.btn-sm {
          height: var(--app-control-height-sm) !important;
          min-height: 0 !important;
          padding: 0 0.55rem !important;
          font-size: 0.75rem !important;
          line-height: normal !important;
          white-space: nowrap !important;
          display: inline-flex !important;
          align-items: center !important;
          justify-content: center !important;
          box-sizing: border-box !important;
          flex-shrink: 0;
          margin: 0 !important;
        }

        input.input-sm,
        select.input-sm {
          height: var(--app-control-height-sm) !important;
          min-height: 0 !important;
          padding: 0 0.55rem !important;
          font-size: 0.75rem !important;
          line-height: normal !important;
          margin: 0 !important;
          box-sizing: border-box !important;
        }

        /* Dashboard cards: flat design, no Pico card shadow */
        .dashboard-root article {
          box-shadow: none;
        }

        /* Compact Table Styling */
        table.compact-table {
          width: 100%;
          min-width: 620px;
          border-collapse: collapse;
          font-size: 0.85rem;
          background: #ffffff;
          border: 1px solid var(--app-border);
          border-radius: var(--app-radius);
          overflow: hidden;
        }

        table.compact-table th {
          background: #f1f5f9;
          color: var(--app-text-muted);
          font-weight: 600;
          text-transform: uppercase;
          font-size: 0.75rem;
          letter-spacing: 0.03em;
          padding: 0.55rem 0.75rem;
          border-bottom: 1px solid var(--app-border);
          white-space: nowrap;
        }

        table.compact-table td {
          padding: 0.5rem 0.75rem;
          border-bottom: 1px solid var(--app-border);
          vertical-align: middle;
        }

        table.compact-table tr:last-child td {
          border-bottom: none;
        }

        table.compact-table tr.selected-row td {
          background-color: #f1f5fd;
        }

        .mobile-only-section {
          display: none !important;
        }

        /* Table column sizing & ellipsis truncation */
        .col-fit {
          width: 1%;
          white-space: nowrap;
        }

        .torrent-title-col {
          width: 100%;
          max-width: 0;
          min-width: 220px;
        }

        .torrent-title-text {
          font-weight: 600;
          color: var(--app-text);
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }

        .hash-copy {
          color: var(--app-text-muted);
          font-size: 0.72rem;
          margin-top: 0.1rem;
          font-family: monospace;
          cursor: pointer;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
        }

        .torrent-file-col {
          width: 100%;
          max-width: 0;
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
          font-size: 0.82rem;
        }

        .btn-icon {
          display: inline-flex;
          align-items: center;
          justify-content: center;
          padding: 0.3rem 0.45rem;
          border-radius: 4px;
          border: 1px solid var(--app-border);
          background: #ffffff;
          color: var(--app-text-muted);
          cursor: pointer;
          transition: all 0.15s ease;
        }

        .btn-icon:hover {
          background: #f8fafc;
          color: var(--app-text);
          border-color: #cbd5e1;
        }

        .btn-icon.danger {
          color: #dc2626;
        }

        .btn-icon.danger:hover {
          background: #fef2f2;
          border-color: #fca5a5;
        }

        .badge {
          display: inline-flex;
          align-items: center;
          padding: 0.15rem 0.4rem;
          font-size: 0.72rem;
          font-weight: 500;
          border-radius: 4px;
          background: #e2e8f0;
          color: #334155;
        }

        .tag-chip {
          cursor: pointer;
          outline: none;
        }

        /* Tag editor pieces match the height of the controls they sit next to */
        .tag-editor .tag-label {
          font-size: 0.75rem;
          font-weight: 600;
          color: var(--app-text-muted);
          line-height: var(--app-control-height-sm);
          white-space: nowrap;
          margin-right: 0.15rem;
        }

        .tag-editor .tag-chip {
          height: var(--app-control-height-sm);
          box-sizing: border-box;
          padding: 0 0.5rem;
          font-size: 0.75rem;
          display: inline-flex;
          align-items: center;
          flex-shrink: 0;
        }

        .tag-chip:focus {
          box-shadow: 0 0 0 2px var(--pico-primary);
          background: #cbd5e1;
        }

        /* Spinner shown next to the result count while the list reloads.
           Always rendered; visibility toggles so the layout never shifts. */
        @keyframes app-spin {
          to { transform: rotate(360deg); }
        }

        .list-spinner {
          display: inline-block;
          width: 0.8rem;
          height: 0.8rem;
          margin-left: 0.5rem;
          border-radius: 50%;
          border: 2px solid var(--app-border);
          border-top-color: var(--pico-primary);
          animation: app-spin 0.7s linear infinite;
          vertical-align: -0.1rem;
        }

        /* The filters pane is always visible above the mobile breakpoint,
           so the drawer toggle button only exists on small screens. */
        @media (min-width: 769px) {
          .mobile-filter-btn {
            display: none !important;
          }
        }

        /* Sidebar: a flex column filling the grid row (which is the height
           remaining in the viewport). The tags list flexes into the space
           left by the categories and scrolls internally. */
        .desktop-sidebar {
          display: flex;
          flex-direction: column;
          min-height: 0;
        }

        .search-form {
          display: flex;
          gap: 0.75rem;
          margin-bottom: 0.85rem;
          flex: none;
        }

        /* Mobile Filter Responsive Drawer */
        @media (max-width: 768px) {
          .search-form {
            margin-bottom: 0.5rem;
          }
          .app-wrapper {
            padding: 0.4rem 0.65rem 0.65rem 0.65rem;
          }
          header.app-header {
            padding: 0;
            margin-bottom: 0.4rem;
          }
          .nav-btn {
            padding: 0.25rem 0.65rem;
            font-size: 0.8rem;
          }
          table.compact-table {
            min-width: 0 !important;
          }
          .torrent-title-col {
            min-width: 0 !important;
          }
          table.compact-table th {
            padding: 0.45rem 0.5rem;
          }
          table.compact-table td {
            padding: 0.45rem 0.5rem;
          }
          .col-select {
            display: none !important;
          }
          .col-hide-mobile {
            display: none !important;
          }
          .expanded-row-toolbar .tag-editor .tag-chip {
            display: none !important;
          }
          .expanded-row-toolbar .search-sort-group {
            flex: 1 1 auto;
            width: auto;
            max-width: none;
          }
          .expanded-row-toolbar .tag-editor {
            flex: 0 0 25%;
            width: 25%;
            max-width: 25%;
            min-width: 5.5rem;
          }
          .expanded-row-toolbar.single-file .tag-editor {
            flex: 0 1 16rem;
            width: 100%;
            max-width: 16rem;
          }
          .mobile-only-section {
            display: block !important;
          }
          .desktop-sidebar {
            display: none !important;
          }
          .desktop-sidebar.mobile-open {
            display: flex !important;
            position: fixed;
            top: 0;
            left: 0;
            width: 280px;
            height: 100vh;
            background: #ffffff;
            z-index: 1000;
            padding: 1rem;
            box-shadow: 2px 0 12px rgba(0,0,0,0.15);
          }
          .mobile-filter-backdrop {
            position: fixed;
            inset: 0;
            background: rgba(0,0,0,0.4);
            z-index: 999;
          }
          .grid-container {
            grid-template-columns: 1fr !important;
          }
          .row-expand-icon {
            display: none !important;
          }
        }
      `}</style>

      <header class="app-header">
        <a href="#" class="brand-logo" onClick={() => setTab('torrents')}>
          <MagnetIcon size={22} color="var(--pico-primary)" strokeWidth={10} />
          <span>Bitmagnet</span>
        </a>

        <nav>
          <ul class="nav-tabs">
            <li>
              <button
                class={`nav-btn ${tab() === 'torrents' ? 'active' : ''}`}
                onClick={() => setTab('torrents')}
              >
                Torrents
              </button>
            </li>
            <li>
              <button
                class={`nav-btn ${tab() === 'admin' ? 'active' : ''}`}
                onClick={() => setTab('admin')}
              >
                Dashboard
              </button>
            </li>
          </ul>
        </nav>
      </header>

      <main>
        {tab() === 'torrents' ? <TorrentList /> : <AdminDashboard />}
      </main>
    </div>
  );
};

const root = document.getElementById('root');
if (root) render(() => <App />, root);
