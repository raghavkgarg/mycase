import { formatINR, formatPct } from '../api.js'

class TaxWarnings extends HTMLElement {
  #ordersListener = null

  connectedCallback() {
    this.innerHTML = `
      <div class="card">
        <p class="card-title">Tax Warnings</p>
        <div id="tax-content"><div class="empty-state">No sell orders — no tax impact.</div></div>
      </div>`
    this.#ordersListener = (e) => this.#onOrders(e.detail)
    document.addEventListener('orders-loaded', this.#ordersListener)
  }

  disconnectedCallback() {
    document.removeEventListener('orders-loaded', this.#ordersListener)
  }

  #onOrders(warnings) {
    const content = this.querySelector('#tax-content')
    if (!content) return
    if (!warnings || warnings.length === 0) {
      content.innerHTML = '<div class="empty-state">No sell orders — no tax impact.</div>'
      return
    }

    const classBadge = (cls) => {
      if (cls === 'STCG') return '<span class="badge badge-stcg">STCG</span>'
      if (cls === 'LTCG') return '<span class="badge badge-ltcg">LTCG</span>'
      return '<span class="badge badge-unknown">UNKNOWN</span>'
    }

    const rows = warnings.map(w => `
      <tr>
        <td><strong>${w.Ticker}</strong></td>
        <td>${classBadge(w.Class === 1 ? 'STCG' : w.Class === 2 ? 'LTCG' : 'UNKNOWN')}</td>
        <td>${w.HoldingDays >= 0 ? w.HoldingDays + ' days' : '—'}</td>
        <td class="${w.EstimatedGain >= 0 ? 'cell-positive' : 'cell-negative'}">${formatINR(w.EstimatedGain)}</td>
        <td class="cell-negative">${w.EstimatedTax > 0 ? formatINR(w.EstimatedTax) : '—'}</td>
      </tr>
      <tr><td colspan="5" class="warn-note" style="padding-top:0;font-size:12px;color:var(--color-muted)">${w.Note || ''}</td></tr>`).join('')

    content.innerHTML = `
      <table class="data-table">
        <thead><tr>
          <th style="text-align:left">Ticker</th>
          <th>Class</th><th>Holding</th>
          <th>Est. Gain</th><th>Est. Tax</th>
        </tr></thead>
        <tbody>${rows}</tbody>
      </table>`
  }
}

customElements.define('tax-warnings', TaxWarnings)
