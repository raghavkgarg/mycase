import { api } from '../api.js'

class MonitorTable extends HTMLElement {
  static observedAttributes = ['portfolio', 'style']

  #portfolio = null
  #style = 'moderate'
  #loading = false

  connectedCallback() {
    this.#render()
    if (this.hasAttribute('portfolio')) {
      this.#portfolio = this.getAttribute('portfolio')
      this.#load()
    }
  }

  attributeChangedCallback(name, _old, val) {
    if (name === 'portfolio' && val !== this.#portfolio) {
      this.#portfolio = val
      this.#load()
    }
    if (name === 'style' && val !== this.#style) {
      this.#style = val
      this.#load()
    }
  }

  #render() {
    this.innerHTML = `
      <div class="card">
        <p class="card-title">Portfolio Monitor</p>
        <div class="monitor-controls">
          <label style="font-size:12px;color:var(--color-muted)">Style:</label>
          <select id="monitor-style">
            <option value="moderate">Moderate</option>
            <option value="hyper-aggressive">Hyper-Aggressive</option>
            <option value="passive">Passive</option>
          </select>
          <button class="btn btn-ghost" id="monitor-refresh" style="font-size:12px;padding:5px 12px">Refresh</button>
        </div>
        <div id="monitor-content"><div class="loading">Select a portfolio to load monitor data…</div></div>
      </div>`

    this.querySelector('#monitor-style').addEventListener('change', (e) => {
      this.#style = e.target.value
      this.#load()
    })
    this.querySelector('#monitor-refresh').addEventListener('click', () => this.#load())
  }

  async #load() {
    if (!this.#portfolio || this.#loading) return
    this.#loading = true
    const content = this.querySelector('#monitor-content')
    content.innerHTML = '<div class="loading">Running simulation… (this may take a moment)</div>'
    try {
      const data = await api.monitor(this.#portfolio, this.#style)
      this.#renderResult(data)
    } catch (err) {
      content.innerHTML = `<div class="empty-state">Failed: ${err.message}</div>`
    } finally {
      this.#loading = false
    }
  }

  #renderResult(data) {
    const content = this.querySelector('#monitor-content')
    const { Verdicts = [], PortfolioReturn = 0, BenchmarkReturn = 0, ExcessReturn = 0, ChurnRate = 0 } = data

    const verdictBadge = (v) => {
      if (v.includes('EXIT')) return `<span class="badge badge-exit">${v}</span>`
      if (v.includes('ALERT')) return `<span class="badge badge-alert">${v}</span>`
      return `<span class="badge badge-hold">${v}</span>`
    }

    const rows = Verdicts.map(v => `
      <tr>
        <td><strong>${v.Ticker}</strong></td>
        <td style="color:var(--color-muted)">${v.Sector || '—'}</td>
        <td class="${v.TTMGrowth >= 0 ? 'cell-positive' : 'cell-negative'}">${v.TTMGrowth?.toFixed(1)}%</td>
        <td class="${v.CAGR3Y >= 0 ? 'cell-positive' : 'cell-negative'}">${v.CAGR3Y?.toFixed(1)}%</td>
        <td class="${v.DSODelta <= 0 ? 'cell-positive' : 'cell-negative'}">${v.DSODelta?.toFixed(1)}%</td>
        <td>${v.CapStallSeverity || '—'}</td>
        <td>${verdictBadge(v.Verdict || '')}</td>
        <td style="color:var(--color-muted);font-size:11px">${v.DataSource || ''}</td>
      </tr>`).join('')

    const exitCount = Verdicts.filter(v => v.Verdict?.includes('EXIT')).length
    const alertCount = Verdicts.filter(v => v.Verdict?.includes('ALERT')).length
    const holdCount = Verdicts.filter(v => v.Verdict?.includes('HOLD')).length

    content.innerHTML = `
      <div class="monitor-summary">
        <span>Hold: <strong style="color:var(--color-success)">${holdCount}</strong></span>
        <span>Alert: <strong style="color:var(--color-warn)">${alertCount}</strong></span>
        <span>Exit: <strong style="color:var(--color-danger)">${exitCount}</strong></span>
        <span style="margin-left:auto">Portfolio: <strong class="${PortfolioReturn >= 0 ? 'cell-positive' : 'cell-negative'}">${PortfolioReturn?.toFixed(2)}%</strong></span>
        <span>Benchmark: <strong>${BenchmarkReturn?.toFixed(2)}%</strong></span>
        <span>Excess: <strong class="${ExcessReturn >= 0 ? 'cell-positive' : 'cell-negative'}">${ExcessReturn?.toFixed(2)}%</strong></span>
        <span>Churn: <strong>${ChurnRate?.toFixed(1)}%</strong></span>
      </div>
      <div style="overflow-x:auto">
        <table class="data-table">
          <thead><tr>
            <th style="text-align:left">Ticker</th>
            <th style="text-align:left">Sector</th>
            <th>TTM Growth</th><th>CAGR 3Y</th><th>DSO Delta</th>
            <th>Cap Stall</th><th>Verdict</th><th>Source</th>
          </tr></thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`
  }
}

customElements.define('monitor-table', MonitorTable)
