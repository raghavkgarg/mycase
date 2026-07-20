import { api, formatINR, formatPct } from '../api.js'

class HoldingsTable extends HTMLElement {
  static observedAttributes = ['portfolio']

  #portfolio = null
  #quoteListener = null

  connectedCallback() {
    this.#render()
    if (this.hasAttribute('portfolio')) {
      this.#portfolio = this.getAttribute('portfolio')
      this.#load()
    }
    this.#quoteListener = (e) => this.#onQuoteUpdate(e.detail)
    document.addEventListener('quote-update', this.#quoteListener)
  }

  disconnectedCallback() {
    if (this.#quoteListener) {
      document.removeEventListener('quote-update', this.#quoteListener)
    }
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
        <p class="card-title">Holdings</p>
        <div class="holdings-wrap">
          <div class="loading">Loading…</div>
        </div>
      </div>`
  }

  async #load() {
    if (!this.#portfolio) return
    const wrap = this.querySelector('.holdings-wrap')
    wrap.innerHTML = '<div class="loading">Loading…</div>'
    try {
      const data = await api.holdings(this.#portfolio)
      this.#renderTable(data)
    } catch (err) {
      wrap.innerHTML = `<div class="empty-state">Failed to load holdings: ${err.message}</div>`
    }
  }

  #renderTable(data) {
    const { holdings = [], summary = {} } = data
    const wrap = this.querySelector('.holdings-wrap')

    let summaryHtml = ''
    if (summary) {
      const driftClass = summary.drift_index < 0.05 ? 'positive' : summary.drift_index < 0.10 ? '' : 'negative'
      summaryHtml = `
        <div class="summary-bar" style="padding:8px 0 16px">
          <div class="summary-item">
            <span class="s-label">Total Value</span>
            <span class="s-value">${formatINR(summary.total_value)}</span>
          </div>
          <div class="summary-item">
            <span class="s-label">Unrealised P&amp;L</span>
            <span class="s-value ${summary.total_pnl >= 0 ? 'cell-positive' : 'cell-negative'}">${formatINR(summary.total_pnl)}</span>
          </div>
          <div class="summary-item">
            <span class="s-label">Drift Index</span>
            <span class="s-value ${driftClass}">${(summary.drift_index * 100).toFixed(2)}%</span>
          </div>
        </div>`
    }

    const rows = holdings.map(h => {
      const devClass = h.deviation > 0.01 ? 'cell-negative' : h.deviation < -0.01 ? 'cell-positive' : ''
      return `<tr>
        <td><strong>${h.ticker}</strong><br><small style="color:var(--color-muted)">${h.exchange}</small></td>
        <td>${h.qty}</td>
        <td>${formatINR(h.avg_cost)}</td>
        <td class="ltp-cell" data-ticker="${h.exchange}:${h.ticker}">${formatINR(h.ltp)}</td>
        <td>${formatINR(h.current_value)}</td>
        <td class="${h.unrealized_pnl >= 0 ? 'cell-positive' : 'cell-negative'}">${formatINR(h.unrealized_pnl)}</td>
        <td class="${h.pnl_pct >= 0 ? 'cell-positive' : 'cell-negative'}">${formatPct(h.pnl_pct)}</td>
        <td>${(h.actual_weight * 100).toFixed(1)}%</td>
        <td>${(h.target_weight * 100).toFixed(1)}%</td>
        <td class="${devClass}">${formatPct(h.deviation * 100)}</td>
      </tr>`
    }).join('')

    wrap.innerHTML = `${summaryHtml}
      <table class="data-table">
        <thead>
          <tr>
            <th style="text-align:left">Ticker</th>
            <th>Qty</th>
            <th>Avg Cost</th>
            <th>LTP</th>
            <th>Value</th>
            <th>Unrealised P&amp;L</th>
            <th>P&amp;L %</th>
            <th>Actual%</th>
            <th>Target%</th>
            <th>Dev%</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>`
  }

  #onQuoteUpdate(quotes) {
    if (!quotes) return
    this.querySelectorAll('.ltp-cell').forEach(cell => {
      const ticker = cell.dataset.ticker
      if (ticker && quotes[ticker] != null) {
        cell.textContent = formatINR(quotes[ticker])
      }
    })
  }
}

customElements.define('holdings-table', HoldingsTable)
