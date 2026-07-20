import { formatPct } from '../api.js'

class MetricsGrid extends HTMLElement {
  #doneListener = null
  #resetListener = null

  connectedCallback() {
    this.innerHTML = `<div class="card"><p class="card-title">Backtest Metrics</p><div class="metrics-grid-inner"><div class="empty-state">Run a backtest to see metrics.</div></div></div>`
    this.#doneListener = (e) => this.#onDone(e.detail)
    this.#resetListener = () => this.#reset()
    document.addEventListener('backtest-done', this.#doneListener)
    document.addEventListener('backtest-reset', this.#resetListener)
  }

  disconnectedCallback() {
    document.removeEventListener('backtest-done', this.#doneListener)
    document.removeEventListener('backtest-reset', this.#resetListener)
  }

  #reset() {
    const grid = this.querySelector('.metrics-grid-inner')
    if (grid) grid.innerHTML = '<div class="empty-state">Run a backtest to see metrics.</div>'
  }

  #onDone(result) {
    if (!result) return
    const metrics = [
      { label: 'CAGR', value: formatPct(result.cagr * 100), cls: result.cagr >= 0 ? 'positive' : 'negative' },
      { label: 'Benchmark CAGR', value: formatPct(result.benchmark_cagr * 100), cls: '' },
      { label: 'Sharpe Ratio', value: result.sharpe?.toFixed(2) ?? '—', cls: '' },
      { label: 'Sortino Ratio', value: result.sortino?.toFixed(2) ?? '—', cls: '' },
      { label: 'Calmar Ratio', value: result.calmar?.toFixed(2) ?? '—', cls: '' },
      { label: 'Max Drawdown', value: formatPct(result.max_drawdown * 100), cls: 'negative' },
      { label: 'Alpha', value: formatPct(result.alpha * 100), cls: result.alpha >= 0 ? 'positive' : 'negative' },
      { label: 'Beta', value: result.beta?.toFixed(2) ?? '—', cls: '' },
    ]
    const grid = this.querySelector('.metrics-grid-inner')
    if (!grid) return
    grid.innerHTML = metrics.map(m => `
      <div class="metric-card">
        <span class="metric-label">${m.label}</span>
        <span class="metric-value ${m.cls}">${m.value}</span>
      </div>`).join('')
  }
}

customElements.define('metrics-grid', MetricsGrid)
