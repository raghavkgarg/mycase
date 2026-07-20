import { api, formatINR, formatPct } from '../api.js'

class OrderPreview extends HTMLElement {
  static observedAttributes = ['portfolio']

  #portfolio = null

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
  }

  #render() {
    this.innerHTML = `
      <div class="card">
        <p class="card-title">Rebalance Orders</p>
        <div id="order-content"><div class="loading">Loading…</div></div>
      </div>`
  }

  async #load() {
    if (!this.#portfolio) return
    const content = this.querySelector('#order-content')
    content.innerHTML = '<div class="loading">Loading…</div>'
    try {
      const data = await api.orders(this.#portfolio)
      this.#renderOrders(data)
      // Notify tax-warnings sibling
      document.dispatchEvent(new CustomEvent('orders-loaded', { detail: data.tax_warnings || [] }))
    } catch (err) {
      content.innerHTML = `<div class="empty-state">Failed: ${err.message}</div>`
    }
  }

  #renderOrders(data) {
    const content = this.querySelector('#order-content')
    const { orders = [], filtered_out = [], summary = {}, tax_warnings = [] } = data

    const rows = orders.map(o => `
      <tr>
        <td><strong>${o.ticker}</strong><small style="color:var(--color-muted)"> ${o.exchange}</small></td>
        <td><span class="badge ${o.action === 'BUY' ? 'badge-buy' : 'badge-sell'}">${o.action}</span></td>
        <td>${o.qty}</td>
        <td>${formatINR(o.ltp)}</td>
        <td>${formatINR(o.price)}</td>
        <td>${formatINR(o.value)}</td>
        <td>${formatINR(o.total_cost)}</td>
        <td>${formatPct(o.cost_ratio * 100)}</td>
      </tr>`).join('')

    const summaryHtml = summary ? `
      <div class="summary-bar" style="padding:10px 0">
        <div class="summary-item"><span class="s-label">Buy</span><span class="s-value cell-positive">${formatINR(summary.total_buy_value)}</span></div>
        <div class="summary-item"><span class="s-label">Sell</span><span class="s-value cell-negative">${formatINR(summary.total_sell_value)}</span></div>
        <div class="summary-item"><span class="s-label">Costs</span><span class="s-value">${formatINR(summary.total_cost)}</span></div>
        <div class="summary-item"><span class="s-label">Cost %</span><span class="s-value">${summary.cost_pct?.toFixed(3)}%</span></div>
      </div>` : ''

    const filteredNote = filtered_out.length > 0
      ? `<p style="font-size:12px;color:var(--color-muted);margin:8px 0">${filtered_out.length} order(s) filtered (cost ratio &gt; 0.5%)</p>`
      : ''

    const executeBtn = `
      <div class="execute-bar">
        <button class="btn btn-danger" id="execute-btn">Execute Orders</button>
        <span style="font-size:12px;color:var(--color-muted)">Live mode only — requires --live flag</span>
      </div>`

    content.innerHTML = `
      ${summaryHtml}
      ${orders.length === 0 ? '<div class="empty-state">No orders needed — portfolio is balanced.</div>' : `
        <div style="overflow-x:auto">
          <table class="data-table">
            <thead><tr>
              <th style="text-align:left">Ticker</th>
              <th>Action</th><th>Qty</th><th>LTP</th><th>Limit</th>
              <th>Value</th><th>Cost</th><th>Cost%</th>
            </tr></thead>
            <tbody>${rows}</tbody>
          </table>
        </div>`}
      ${filteredNote}
      ${executeBtn}`

    this.querySelector('#execute-btn')?.addEventListener('click', () => this.#onExecute())
  }

  async #onExecute() {
    if (!this.#portfolio) return
    if (!confirm('Place live orders? This cannot be undone.')) return
    const btn = this.querySelector('#execute-btn')
    btn.disabled = true
    btn.textContent = 'Placing…'
    try {
      const result = await api.execute(this.#portfolio)
      const placed = result.placed?.length || 0
      const errs = result.errors?.length || 0
      alert(`Placed: ${placed} order(s). Errors: ${errs}.${errs > 0 ? '\n' + result.errors.join('\n') : ''}`)
    } catch (err) {
      alert(`Execute failed: ${err.message}`)
    } finally {
      btn.disabled = false
      btn.textContent = 'Execute Orders'
    }
  }
}

customElements.define('order-preview', OrderPreview)
