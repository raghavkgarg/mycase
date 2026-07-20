import { api, formatINR, formatPct } from '../api.js'

class DriftTimeline extends HTMLElement {
  static observedAttributes = ['portfolio']

  #portfolio = null
  #chart = null

  connectedCallback() {
    this.#render()
    if (this.hasAttribute('portfolio')) {
      this.#portfolio = this.getAttribute('portfolio')
      this.#load()
    }
  }

  disconnectedCallback() {
    this.#chart?.dispose()
    this.#chart = null
  }

  attributeChangedCallback(name, _old, val) {
    if (name === 'portfolio' && val !== this.#portfolio) {
      this.#portfolio = val
      this.#load()
    }
  }

  #render() {
    this.innerHTML = `
      <div class="card">
        <p class="card-title">Drift Analysis</p>
        <div id="drift-content"><div class="loading">Loading…</div></div>
      </div>`
  }

  async #load() {
    if (!this.#portfolio) return
    const content = this.querySelector('#drift-content')
    content.innerHTML = '<div class="loading">Loading…</div>'
    try {
      const [driftData, history] = await Promise.all([
        api.drift(this.#portfolio),
        api.daemonHistory(),
      ])
      this.#renderDrift(driftData, history)
    } catch (err) {
      content.innerHTML = `<div class="empty-state">Failed: ${err.message}</div>`
    }
  }

  #renderDrift(drift, history) {
    const content = this.querySelector('#drift-content')
    if (!drift) {
      content.innerHTML = '<div class="empty-state">No drift data.</div>'
      return
    }

    const idx = drift.DriftIndex ?? 0
    const driftPct = (idx * 100).toFixed(2)
    const gaugeClass = idx < 0.05 ? 'drift-low' : idx < 0.10 ? 'drift-mid' : 'drift-high'
    const gaugeWidth = Math.min(100, idx * 200).toFixed(1)

    // Per-ticker drift table
    const keys = drift.BasketKeys || []
    const actual = drift.ActualWeights || {}
    const target = drift.TargetWeights || {}

    const tickerRows = keys.map(k => {
      const aw = (actual[k] || 0) * 100
      const tw = (target[k] || 0) * 100
      const dev = aw - tw
      const parts = k.split(':')
      const sym = parts[parts.length - 1]
      const dir = dev > 0.5 ? '▲ Over' : dev < -0.5 ? '▼ Under' : '≈ On target'
      const dirClass = dev > 0.5 ? 'cell-negative' : dev < -0.5 ? 'cell-positive' : ''
      return `<tr>
        <td><strong>${sym}</strong></td>
        <td>${aw.toFixed(2)}%</td>
        <td>${tw.toFixed(2)}%</td>
        <td class="${dev > 0 ? 'cell-negative' : dev < 0 ? 'cell-positive' : ''}">${formatPct(dev)}</td>
        <td class="${dirClass}">${dir}</td>
      </tr>`
    }).join('')

    // Daemon history summary
    const histHtml = history && history.last_check_at ? `
      <div style="margin-top:16px;font-size:12px;color:var(--color-muted)">
        Last daemon check: ${new Date(history.last_check_at).toLocaleString()}<br/>
        Last drift: ${((history.last_drift || 0) * 100).toFixed(2)}% ·
        Alerts sent: ${history.alerts_sent || 0} ·
        Portfolio: ${history.portfolio_file || '—'}
      </div>` : ''

    content.innerHTML = `
      <div class="drift-header">
        <span class="drift-index-value">${driftPct}%</span>
        <span style="color:var(--color-muted);font-size:13px">Drift Index</span>
        ${formatINR(drift.TotalValue)} total ·
        <span style="font-size:12px;color:var(--color-muted)">${new Date(drift.CheckedAt).toLocaleTimeString()}</span>
      </div>
      <div class="${gaugeClass}" style="margin-bottom:16px">
        <div class="drift-gauge">
          <div class="drift-gauge-fill" style="width:${gaugeWidth}%"></div>
        </div>
        <span style="font-size:11px;color:var(--color-muted)">${idx < 0.05 ? 'Low drift' : idx < 0.10 ? 'Moderate drift' : 'High drift — rebalance recommended'}</span>
      </div>
      ${keys.length > 0 ? `
        <div style="overflow-x:auto">
          <table class="data-table">
            <thead><tr>
              <th style="text-align:left">Ticker</th>
              <th>Actual%</th><th>Target%</th><th>Deviation</th><th>Direction</th>
            </tr></thead>
            <tbody>${tickerRows}</tbody>
          </table>
        </div>` : ''}
      ${histHtml}`
  }
}

customElements.define('drift-timeline', DriftTimeline)
