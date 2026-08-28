import { api, formatUSD } from '../api.js'

class TaxTab extends HTMLElement {
  #portfolio = null
  #reloadListener = null

  static get observedAttributes() { return ['portfolio'] }

  connectedCallback() {
    this.innerHTML = `
      <div class="view-grid" style="gap: 20px;">
        <div class="card">
          <p class="card-title">Realized Gains / Losses</p>
          <div id="tax-realized"><div class="empty-state">Loading…</div></div>
        </div>
        <div class="card">
          <p class="card-title">Tax-Loss Harvesting Candidates</p>
          <div id="tax-harvest"><div class="empty-state">Loading…</div></div>
        </div>
        <div class="card">
          <p class="card-title">Wash-Sale Calendar (buys within 30 days)</p>
          <div id="tax-wash"><div class="empty-state">Loading…</div></div>
        </div>
        <div class="card">
          <p class="card-title">Open Lots</p>
          <div id="tax-lots"><div class="empty-state">Loading…</div></div>
        </div>
      </div>`
    this.#reloadListener = () => this.#load()
    document.addEventListener('portfolio-reload', this.#reloadListener)
    if (this.#portfolio) this.#load()
  }

  disconnectedCallback() {
    document.removeEventListener('portfolio-reload', this.#reloadListener)
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
      data = await api.tax(this.#portfolio)
    } catch (e) {
      this.#setEmpty('Failed to load tax data: ' + e.message)
      return
    }

    if (!data || !data.available) {
      this.#setEmpty(data && data.message ? data.message : 'No tax data available.')
      return
    }

    this.#renderRealized(data)
    this.#renderHarvest(data)
    this.#renderWash(data.wash_sale_calendar || [])
    this.#renderLots(data.lots || [])
  }

  #setEmpty(msg) {
    for (const id of ['tax-realized', 'tax-harvest', 'tax-wash', 'tax-lots']) {
      const el = this.querySelector('#' + id)
      if (el) el.innerHTML = `<div class="empty-state">${msg}</div>`
    }
  }

  #renderRealized(data) {
    const el = this.querySelector('#tax-realized')
    if (!el) return
    const row = (label, s) => `
      <tr>
        <td><strong>${label}</strong></td>
        <td class="cell-positive">${formatUSD(s.short_term_gain)}</td>
        <td class="cell-negative">${formatUSD(s.short_term_loss)}</td>
        <td class="cell-positive">${formatUSD(s.long_term_gain)}</td>
        <td class="cell-negative">${formatUSD(s.long_term_loss)}</td>
        <td class="${s.net_total >= 0 ? 'cell-positive' : 'cell-negative'}"><strong>${formatUSD(s.net_total)}</strong></td>
      </tr>`
    el.innerHTML = `
      <table class="data-table">
        <thead><tr>
          <th style="text-align:left">Period</th>
          <th>ST Gain</th><th>ST Loss</th>
          <th>LT Gain</th><th>LT Loss</th>
          <th>Net</th>
        </tr></thead>
        <tbody>
          ${row('YTD', data.realized_ytd)}
          ${row('All time', data.realized_all_time)}
        </tbody>
      </table>`
  }

  #renderHarvest(data) {
    const el = this.querySelector('#tax-harvest')
    if (!el) return
    const rows = data.harvest_candidates || []
    if (rows.length === 0) {
      el.innerHTML = '<div class="empty-state">No harvestable losses above threshold.</div>'
      return
    }
    const body = rows.map(h => `
      <tr>
        <td><strong>${h.ticker}</strong></td>
        <td>${h.quantity.toFixed(2)}</td>
        <td class="cell-negative">${formatUSD(h.unrealized_loss)}</td>
        <td class="cell-positive">${formatUSD(h.est_tax_saving)}</td>
        <td>${h.long_term ? '<span class="badge badge-ltcg">LT</span>' : '<span class="badge badge-stcg">ST</span>'}</td>
        <td>${h.wash_sale_risk ? '<span class="badge badge-unknown">⚠️ WASH</span>' : ''}</td>
        <td>${h.substitute || '—'}</td>
      </tr>`).join('')
    el.innerHTML = `
      <table class="data-table">
        <thead><tr>
          <th style="text-align:left">Ticker</th>
          <th>Qty</th><th>Unrealized Loss</th><th>Est. Tax Saving</th>
          <th>Term</th><th>Wash?</th><th>Substitute</th>
        </tr></thead>
        <tbody>${body}</tbody>
      </table>
      <p style="margin-top:10px;font-size:13px">Total estimated tax saving: <strong class="cell-positive">${formatUSD(data.harvest_total_est)}</strong></p>`
  }

  #renderWash(rows) {
    const el = this.querySelector('#tax-wash')
    if (!el) return
    if (rows.length === 0) {
      el.innerHTML = '<div class="empty-state">No recent buys inside the 30-day wash-sale window.</div>'
      return
    }
    const body = rows.map(w => `
      <tr>
        <td><strong>${w.ticker}</strong></td>
        <td>${w.buy_date}</td>
        <td>${w.days_apart} days ago</td>
        <td style="font-size:12px;color:var(--color-muted)">${w.note}</td>
      </tr>`).join('')
    el.innerHTML = `
      <table class="data-table">
        <thead><tr>
          <th style="text-align:left">Ticker</th>
          <th>Last Buy</th><th>Age</th><th>Note</th>
        </tr></thead>
        <tbody>${body}</tbody>
      </table>`
  }

  #renderLots(rows) {
    const el = this.querySelector('#tax-lots')
    if (!el) return
    if (rows.length === 0) {
      el.innerHTML = '<div class="empty-state">No open lots.</div>'
      return
    }
    const body = rows.map(l => `
      <tr>
        <td><strong>${l.ticker}</strong></td>
        <td>${l.acquired}</td>
        <td>${l.quantity.toFixed(2)}</td>
        <td>${formatUSD(l.cost_per_share)}</td>
        <td>${formatUSD(l.cost_basis)}</td>
        <td>${formatUSD(l.market_value)}</td>
        <td class="${l.unrealized >= 0 ? 'cell-positive' : 'cell-negative'}">${formatUSD(l.unrealized)}</td>
        <td>${l.long_term ? '<span class="badge badge-ltcg">LT</span>' : '<span class="badge badge-stcg">ST</span>'}</td>
      </tr>`).join('')
    el.innerHTML = `
      <table class="data-table">
        <thead><tr>
          <th style="text-align:left">Ticker</th>
          <th>Acquired</th><th>Qty</th><th>Cost/Sh</th>
          <th>Basis</th><th>Mkt Value</th><th>Unrealized</th><th>Term</th>
        </tr></thead>
        <tbody>${body}</tbody>
      </table>`
  }
}

customElements.define('tax-tab', TaxTab)
