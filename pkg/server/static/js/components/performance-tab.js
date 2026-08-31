import { api, formatUSD, formatPct } from '../api.js'

// PerformanceTab renders live performance attribution vs a passive benchmark:
// an equity curve (portfolio vs SPY), vs-benchmark metrics, and a
// selection/rebalancing return decomposition. Mirrors tax-tab.js structure
// (portfolio attribute + portfolio-reload listener + available-flag guard).
class PerformanceTab extends HTMLElement {
  #portfolio = null
  #reloadListener = null
  #chart = null

  static get observedAttributes() { return ['portfolio'] }

  connectedCallback() {
    this.innerHTML = `
      <div class="view-grid" style="gap: 20px;">
        <div class="card">
          <p class="card-title">Portfolio vs Benchmark</p>
          <div id="perf-curve" class="chart-container tall"></div>
        </div>
        <div class="card">
          <p class="card-title">Vs-Benchmark Metrics</p>
          <div id="perf-metrics"><div class="empty-state">Loading…</div></div>
        </div>
        <div class="card">
          <p class="card-title">Return Decomposition</p>
          <div id="perf-decomp"><div class="empty-state">Loading…</div></div>
        </div>
      </div>`
    this.#reloadListener = () => this.#load()
    document.addEventListener('portfolio-reload', this.#reloadListener)
    if (this.#portfolio) this.#load()
  }

  disconnectedCallback() {
    document.removeEventListener('portfolio-reload', this.#reloadListener)
    this.#chart?.dispose()
    this.#chart = null
  }

  attributeChangedCallback(name, _old, val) {
    if (name === 'portfolio') {
      this.#portfolio = val
      if (this.isConnected) this.#load()
    }
  }

  async #load() {
    if (!this.#portfolio) return
    let data
    try {
      data = await api.performance(this.#portfolio)
    } catch (e) {
      this.#setEmpty('Failed to load performance data: ' + e.message)
      return
    }
    if (!data || !data.available) {
      this.#setEmpty(data && data.message ? data.message : 'No performance data available.')
      return
    }
    this.#renderCurve(data.nav_series || [], data.benchmark || 'Benchmark')
    this.#renderMetrics(data.metrics || {}, data.benchmark || 'Benchmark')
    this.#renderDecomp(data.decomposition)
  }

  #setEmpty(msg) {
    for (const id of ['perf-metrics', 'perf-decomp']) {
      const el = this.querySelector('#' + id)
      if (el) el.innerHTML = `<div class="empty-state">${msg}</div>`
    }
    const curve = this.querySelector('#perf-curve')
    if (curve) curve.innerHTML = `<div class="empty-state">${msg}</div>`
  }

  #renderCurve(series, benchLabel) {
    const container = this.querySelector('#perf-curve')
    if (!container || !window.echarts) return
    if (series.length === 0) {
      container.innerHTML = '<div class="empty-state">No NAV series.</div>'
      return
    }
    const dates = series.map(p => p.date)
    const base = series[0].portfolio || 1
    const benchBase = series[0].benchmark || 1
    const portNorm = series.map(p => +((p.portfolio / base * 100).toFixed(2)))
    const benchNorm = series.map(p => +((p.benchmark / benchBase * 100).toFixed(2)))

    if (!this.#chart) this.#chart = window.echarts.init(container, 'dark')
    this.#chart.setOption({
      backgroundColor: 'transparent',
      tooltip: { trigger: 'axis' },
      legend: { data: ['Portfolio', benchLabel], textStyle: { color: '#8892a4' } },
      grid: { left: 50, right: 20, top: 40, bottom: 30 },
      xAxis: { type: 'category', data: dates, axisLabel: { color: '#8892a4', fontSize: 10, interval: 'auto' } },
      yAxis: { type: 'value', axisLabel: { formatter: '{value}', color: '#8892a4' } },
      series: [
        { name: 'Portfolio', type: 'line', data: portNorm, smooth: false, symbol: 'none', lineStyle: { color: '#4f8ef7', width: 2 }, areaStyle: { color: 'rgba(79,142,247,0.08)' } },
        { name: benchLabel, type: 'line', data: benchNorm, smooth: false, symbol: 'none', lineStyle: { color: '#8892a4', width: 1.5, type: 'dashed' } },
      ],
    })
    this.#chart.resize()
  }

  #renderMetrics(m, benchLabel) {
    const el = this.querySelector('#perf-metrics')
    if (!el) return
    const pct = (v) => formatPct((v || 0) * 100)
    const num = (v) => (v == null || isNaN(v)) ? '—' : v.toFixed(3)
    const cls = (v) => (v >= 0 ? 'cell-positive' : 'cell-negative')
    el.innerHTML = `
      <table class="data-table">
        <tbody>
          <tr><td>Period</td><td>${m.from || '—'} → ${m.to || '—'} (${m.trading_days || 0} days)</td></tr>
          <tr><td>Portfolio final</td><td>${formatUSD(m.final_value)} <span class="${cls(m.total_return)}">(${pct(m.total_return)})</span></td></tr>
          <tr><td>${benchLabel} final</td><td>${formatUSD(m.benchmark_final)} <span class="${cls(m.benchmark_return)}">(${pct(m.benchmark_return)})</span></td></tr>
          <tr><td>Alpha (annualized)</td><td class="${cls(m.alpha)}">${pct(m.alpha)}</td></tr>
          <tr><td>Beta</td><td>${num(m.beta)}</td></tr>
          <tr><td>Information ratio</td><td>${num(m.information_ratio)}</td></tr>
          <tr><td>Tracking error</td><td>${pct(m.tracking_error)}</td></tr>
          <tr><td>Max drawdown</td><td class="cell-negative">${pct(m.max_drawdown)}</td></tr>
          <tr><td>Sharpe ratio</td><td>${num(m.sharpe)}</td></tr>
        </tbody>
      </table>`
  }

  #renderDecomp(d) {
    const el = this.querySelector('#perf-decomp')
    if (!el) return
    if (!d) {
      el.innerHTML = '<div class="empty-state">Decomposition unavailable (no rebalance history).</div>'
      return
    }
    const pct = (v) => formatPct((v || 0) * 100)
    const cls = (v) => (v >= 0 ? 'cell-positive' : 'cell-negative')
    el.innerHTML = `
      <table class="data-table">
        <thead><tr><th style="text-align:left">Component</th><th>Contribution</th><th style="text-align:left">Meaning</th></tr></thead>
        <tbody>
          <tr><td><strong>Active return</strong></td><td class="${cls(d.active_return)}"><strong>${pct(d.active_return)}</strong></td><td>portfolio − benchmark</td></tr>
          <tr><td>Selection effect</td><td class="${cls(d.selection)}">${pct(d.selection)}</td><td>picks vs index (first basket held)</td></tr>
          <tr><td>Rebalancing effect</td><td class="${cls(d.rebalancing)}">${pct(d.rebalancing)}</td><td>re-selection vs holding first basket</td></tr>
        </tbody>
      </table>
      <p style="margin-top:10px;font-size:12px;color:var(--color-muted)">
        ${d.rebalances} rebalance${d.rebalances === 1 ? '' : 's'} in window · Selection + Rebalancing = Active return
      </p>
      <p style="margin-top:4px;font-size:12px;color:var(--color-muted)">
        Tax-loss harvesting impact is reported separately in the Tax tab.
      </p>`
  }
}

customElements.define('performance-tab', PerformanceTab)
