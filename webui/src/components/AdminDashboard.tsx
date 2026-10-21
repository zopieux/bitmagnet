import { createSignal, onMount, onCleanup, createEffect, Component } from 'solid-js';
import { Chart, registerables } from 'chart.js';
import { getAdminMetrics, AdminDashboardMetrics, TimeframeMode } from '../api/graphql';

Chart.register(...registerables);

export const AdminDashboard: Component = () => {
  let canvasRef: HTMLCanvasElement | undefined;
  let chartInstance: Chart | undefined;

  const [timeframe, setTimeframe] = createSignal<TimeframeMode>('hourly');
  const [metrics, setMetrics] = createSignal<AdminDashboardMetrics>({
    totalProcessedTorrents: 0,
    pendingQueueSize: 0,
    retryQueueSize: 0,
    discoveredRatePerMin: 0,
    processedRatePerMin: 0,
    failingRatePerMin: 0,
    historyPoints: [],
  });

  const [lastUpdated, setLastUpdated] = createSignal<string>('');
  const [loading, setLoading] = createSignal<boolean>(false);

  const fetchMetrics = async () => {
    setLoading(true);
    try {
      const data = await getAdminMetrics(timeframe());
      setMetrics(data);
      setLastUpdated(new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }));
    } catch (e) {
      console.error('Failed to update admin dashboard metrics:', e);
    } finally {
      setLoading(false);
    }
  };

  createEffect(() => {
    fetchMetrics();
  });

  createEffect(() => {
    const data = metrics().historyPoints;
    if (!canvasRef) return;

    const labels = data.map((d) => d.label);
    const discoveredData = data.map((d) => d.discovered);
    const processedData = data.map((d) => d.processed);
    const failingData = data.map((d) => d.failing);

    if (chartInstance) {
      chartInstance.data.labels = labels;
      chartInstance.data.datasets[0].data = discoveredData;
      chartInstance.data.datasets[1].data = processedData;
      chartInstance.data.datasets[2].data = failingData;
      chartInstance.update();
    } else {
      chartInstance = new Chart(canvasRef, {
        type: 'line',
        data: {
          labels,
          datasets: [
            {
              label: 'Discovered',
              data: discoveredData,
              borderColor: '#2563eb',
              backgroundColor: 'rgba(37, 99, 235, 0.4)',
              fill: true,
              tension: 0.3,
              pointRadius: 1,
            },
            {
              label: 'Processed',
              data: processedData,
              borderColor: '#16a34a',
              backgroundColor: 'rgba(22, 163, 74, 0.4)',
              fill: true,
              tension: 0.3,
              pointRadius: 1,
            },
            {
              label: 'Failing / Retrying',
              data: failingData,
              borderColor: '#dc2626',
              backgroundColor: 'rgba(220, 38, 38, 0.4)',
              fill: true,
              tension: 0.3,
              pointRadius: 1,
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          interaction: {
            mode: 'index',
            intersect: false,
          },
          scales: {
            x: {
              stacked: true,
              grid: {
                color: 'rgba(0, 0, 0, 0.05)',
              },
              ticks: {
                maxRotation: 0,
                autoSkip: true,
                maxTicksLimit: 12,
              },
            },
            y: {
              stacked: true,
              beginAtZero: true,
              grid: {
                color: 'rgba(0, 0, 0, 0.05)',
              },
            },
          },
          plugins: {
            legend: {
              position: 'top',
              labels: {
                usePointStyle: true,
                boxWidth: 8,
              },
            },
            tooltip: {
              mode: 'index',
            },
          },
        },
      });
    }
  });

  onMount(() => {
    const interval = setInterval(fetchMetrics, 10000);
    onCleanup(() => {
      clearInterval(interval);
      if (chartInstance) {
        chartInstance.destroy();
      }
    });
  });

  return (
    <div class="dashboard-root">
      <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-bottom': '1.25rem' }}>
        <div>
          <h3 style={{ margin: 0, 'font-size': '1.15rem', 'font-weight': '600' }}>Dashboard</h3>
          <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.78rem' }}>
            Auto-refreshes every 10s {lastUpdated() ? `• Last updated at ${lastUpdated()}` : ''}
          </span>
        </div>
        <button
          class="nav-btn active"
          onClick={fetchMetrics}
          disabled={loading()}
          style={{ display: 'inline-flex', 'align-items': 'center', gap: '0.4rem' }}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 12a9 9 0 0 1 15-6.7L21 8"/>
            <path d="M21 3v5h-5"/>
            <path d="M21 12a9 9 0 0 1-15 6.7L3 16"/>
            <path d="M3 21v-5h5"/>
          </svg>
          <span>{loading() ? 'Refreshing...' : 'Refresh'}</span>
        </button>
      </div>

      <div style={{ display: 'grid', 'grid-template-columns': 'repeat(auto-fit, minmax(260px, 1fr))', gap: '1rem', 'margin-bottom': '1.5rem' }}>
        <article style={{ background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', padding: '1rem', margin: 0 }}>
          <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'font-weight': '600', 'text-transform': 'uppercase', 'letter-spacing': '0.04em' }}>
            Est. Total Processed Torrents
          </span>
          <div style={{ 'font-size': '1.6rem', 'font-weight': '700', 'margin-top': '0.4rem', color: 'var(--pico-primary)' }}>
            {metrics().totalProcessedTorrents.toLocaleString()}
          </div>
        </article>

        <article style={{ background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', padding: '1rem', margin: 0 }}>
          <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'font-weight': '600', 'text-transform': 'uppercase', 'letter-spacing': '0.04em' }}>
            Pending Queue Size
          </span>
          <div style={{ 'font-size': '1.6rem', 'font-weight': '700', 'margin-top': '0.4rem', color: '#ea580c' }}>
            {metrics().pendingQueueSize.toLocaleString()}
          </div>
        </article>

        <article style={{ background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', padding: '1rem', margin: 0 }}>
          <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'font-weight': '600', 'text-transform': 'uppercase', 'letter-spacing': '0.04em' }}>
            Retry Queue Size
          </span>
          <div style={{ 'font-size': '1.6rem', 'font-weight': '700', 'margin-top': '0.4rem', color: '#dc2626' }}>
            {metrics().retryQueueSize.toLocaleString()}
          </div>
        </article>
      </div>

      <h4 style={{ 'font-size': '0.95rem', 'font-weight': '600', 'margin-bottom': '0.75rem' }}>Current Rates (per minute)</h4>
      <div style={{ display: 'grid', 'grid-template-columns': 'repeat(auto-fit, minmax(260px, 1fr))', gap: '1rem', 'margin-bottom': '1.5rem' }}>
        <article style={{ background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', padding: '1rem', margin: 0 }}>
          <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'font-weight': '600', 'text-transform': 'uppercase', 'letter-spacing': '0.04em' }}>
            Discovered Torrents
          </span>
          <div style={{ 'font-size': '1.6rem', 'font-weight': '700', 'margin-top': '0.4rem', color: '#2563eb' }}>
            {metrics().discoveredRatePerMin.toLocaleString()} <span style={{ 'font-size': '0.85rem', 'font-weight': '400', color: 'var(--app-text-muted)' }}>/ min</span>
          </div>
        </article>

        <article style={{ background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', padding: '1rem', margin: 0 }}>
          <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'font-weight': '600', 'text-transform': 'uppercase', 'letter-spacing': '0.04em' }}>
            Processed Torrents
          </span>
          <div style={{ 'font-size': '1.6rem', 'font-weight': '700', 'margin-top': '0.4rem', color: '#16a34a' }}>
            {metrics().processedRatePerMin.toLocaleString()} <span style={{ 'font-size': '0.85rem', 'font-weight': '400', color: 'var(--app-text-muted)' }}>/ min</span>
          </div>
        </article>

        <article style={{ background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', padding: '1rem', margin: 0 }}>
          <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem', 'font-weight': '600', 'text-transform': 'uppercase', 'letter-spacing': '0.04em' }}>
            Failing / Retrying
          </span>
          <div style={{ 'font-size': '1.6rem', 'font-weight': '700', 'margin-top': '0.4rem', color: '#dc2626' }}>
            {metrics().failingRatePerMin.toLocaleString()} <span style={{ 'font-size': '0.85rem', 'font-weight': '400', color: 'var(--app-text-muted)' }}>/ min</span>
          </div>
        </article>
      </div>

      <article style={{ background: '#ffffff', border: '1px solid var(--app-border)', 'border-radius': 'var(--app-radius)', padding: '1.25rem', margin: 0 }}>
        <div style={{ display: 'flex', 'justify-content': 'space-between', 'align-items': 'center', 'margin-bottom': '1rem' }}>
          <div>
            <h4 style={{ 'font-size': '0.95rem', 'font-weight': '600', margin: 0 }}>Processing Rates (96 points)</h4>
            <span style={{ color: 'var(--app-text-muted)', 'font-size': '0.75rem' }}>
              {timeframe() === 'hourly' && '24-hour depth (15m interval)'}
              {timeframe() === 'daily' && '4-day depth (1h interval)'}
              {timeframe() === 'weekly' && '28-day depth (7h interval)'}
            </span>
          </div>
          <div role="group" style={{ display: 'inline-flex', gap: '0.25rem', margin: 0 }}>
            <button
              class={`nav-btn ${timeframe() === 'hourly' ? 'active' : ''}`}
              style={{ padding: '0.25rem 0.6rem', 'font-size': '0.78rem' }}
              onClick={() => setTimeframe('hourly')}
            >
              Hourly (24h)
            </button>
            <button
              class={`nav-btn ${timeframe() === 'daily' ? 'active' : ''}`}
              style={{ padding: '0.25rem 0.6rem', 'font-size': '0.78rem' }}
              onClick={() => setTimeframe('daily')}
            >
              Daily (4d)
            </button>
            <button
              class={`nav-btn ${timeframe() === 'weekly' ? 'active' : ''}`}
              style={{ padding: '0.25rem 0.6rem', 'font-size': '0.78rem' }}
              onClick={() => setTimeframe('weekly')}
            >
              Weekly (28d)
            </button>
          </div>
        </div>
        <div style={{ height: '320px', position: 'relative' }}>
          <canvas ref={canvasRef} />
        </div>
      </article>
    </div>
  );
};
