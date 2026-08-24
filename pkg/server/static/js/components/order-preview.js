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

    // Check for pending autopilot proposal first
    try {
      const proposalData = await api.autopilotProposal()
      if (proposalData.proposal && proposalData.proposal.status === 'pending') {
        this.#renderProposal(proposalData.proposal)
        return
      }
    } catch (_) {
      // If autopilot API fails, fall through to live order computation
    }

    // Default: live order computation
    try {
      const data = await api.orders(this.#portfolio)
      this.#renderOrders(data)
      document.dispatchEvent(new CustomEvent('orders-loaded', { detail: data.tax_warnings || [] }))
    } catch (err) {
      content.innerHTML = `<div class="empty-state">Failed: ${err.message}</div>`
    }
  }

  #renderProposal(proposal) {
    const content = this.querySelector('#order-content')
    const orders = proposal.orders || []
    const filtered = proposal.filtered_out || []
    const entries = proposal.entries || []
    const exits = proposal.exits || []

    // Expiry info
    const expiresAt = new Date(proposal.expires_at)
    const hoursLeft = Math.max(0, (expiresAt - Date.now()) / 3600000).toFixed(0)

    // Changes section
    let changesHtml = ''
    if (entries.length > 0 || exits.length > 0) {
      changesHtml = `<div class="proposal-changes" style="margin-bottom:12px;padding:10px;border-radius:6px;background:var(--color-surface-alt,#1a1f2e)">
        <strong style="font-size:13px">Portfolio Changes</strong>
        ${entries.map(e => `<div style="color:var(--color-positive,#4ade80);font-size:12px;margin:2px 0">+ ${e.ticker} (${(e.weight*100).toFixed(1)}%) — ${e.reason || 'New entry'}</div>`).join('')}
        ${exits.map(e => `<div style="color:var(--color-negative,#f87171);font-size:12px;margin:2px 0">− ${e.ticker} — ${e.reason || 'Exited'}</div>`).join('')}
      </div>`
    }

    // Summary bar
    const summaryHtml = `
      <div class="summary-bar" style="padding:10px 0">
        <div class="summary-item"><span class="s-label">Buy</span><span class="s-value cell-positive">${formatINR(proposal.total_buy_value)}</span></div>
        <div class="summary-item"><span class="s-label">Sell</span><span class="s-value cell-negative">${formatINR(proposal.total_sell_value)}</span></div>
        <div class="summary-item"><span class="s-label">Est. Cost</span><span class="s-value">${formatINR(proposal.estimated_cost)}</span></div>
        <div class="summary-item"><span class="s-label">Expires</span><span class="s-value">${hoursLeft}h</span></div>
      </div>`

    // Orders table
    const rows = orders.map(o => `
      <tr>
        <td><strong>${o.ticker}</strong><small style="color:var(--color-muted)"> ${o.exchange}</small></td>
        <td><span class="badge ${o.action === 'BUY' ? 'badge-buy' : 'badge-sell'}">${o.action}</span></td>
        <td>${o.quantity}</td>
        <td>${formatINR(o.limit_price)}</td>
        <td>${formatINR(o.value)}</td>
      </tr>`).join('')

    const filteredNote = filtered.length > 0
      ? `<p style="font-size:12px;color:var(--color-muted);margin:8px 0">${filtered.length} order(s) filtered (micro-transaction)</p>`
      : ''

    // Tax warnings
    const taxWarnings = (proposal.tax_warnings || [])
    const taxHtml = taxWarnings.length > 0
      ? `<div style="margin:8px 0;padding:8px;border-radius:4px;background:rgba(251,191,36,0.1);font-size:12px">
          <strong>⚠️ Tax Impact:</strong>
          ${taxWarnings.map(tw => `<div style="margin:2px 0">${tw}</div>`).join('')}
        </div>`
      : ''

    // Action buttons
    const actionsHtml = `
      <div class="execute-bar" style="display:flex;gap:12px;margin-top:12px">
        <button class="btn btn-danger" id="confirm-proposal-btn">✓ Confirm & Execute</button>
        <button class="btn btn-secondary" id="dismiss-proposal-btn" style="background:var(--color-surface-alt,#374151);color:var(--color-text)">✗ Dismiss</button>
      </div>
      <p style="font-size:11px;color:var(--color-muted);margin-top:6px">
        Proposal <code>${proposal.id.slice(0,8)}</code> from autopilot run on ${new Date(proposal.created_at).toLocaleDateString()}.
        Strategy: ${proposal.strategy}. Expires: ${expiresAt.toLocaleDateString()}.
      </p>`

    content.innerHTML = `
      <div style="padding:8px 12px;margin-bottom:10px;border-radius:6px;background:rgba(59,130,246,0.1);border:1px solid rgba(59,130,246,0.3)">
        <strong>📊 Autopilot Proposal Ready</strong>
        <span style="font-size:12px;color:var(--color-muted);margin-left:8px">${proposal.frequency} rebalance</span>
      </div>
      ${changesHtml}
      ${summaryHtml}
      ${orders.length === 0 ? '<div class="empty-state">No orders in proposal.</div>' : `
        <div style="overflow-x:auto">
          <table class="data-table">
            <thead><tr>
              <th style="text-align:left">Ticker</th>
              <th>Action</th><th>Qty</th><th>Limit</th><th>Value</th>
            </tr></thead>
            <tbody>${rows}</tbody>
          </table>
        </div>`}
      ${filteredNote}
      ${taxHtml}
      ${actionsHtml}`

    // Wire up buttons
    this.querySelector('#confirm-proposal-btn')?.addEventListener('click', () => this.#onConfirmProposal())
    this.querySelector('#dismiss-proposal-btn')?.addEventListener('click', () => this.#onDismissProposal())

    // Dispatch tax warnings for sibling component
    document.dispatchEvent(new CustomEvent('orders-loaded', { detail: taxWarnings }))
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

  async #onConfirmProposal() {
    if (!confirm('Confirm autopilot rebalance? This will place live orders.')) return
    const btn = this.querySelector('#confirm-proposal-btn')
    btn.disabled = true
    btn.textContent = 'Placing orders…'
    try {
      const result = await api.autopilotConfirm()
      const placed = result.placed || 0
      const failed = result.failed || 0
      alert(`✅ Rebalance executed. ${placed} order(s) placed.${failed > 0 ? ` ${failed} failed.` : ''}`)
      this.#load() // Reload to show fresh state
    } catch (err) {
      alert(`Execute failed: ${err.message}`)
      btn.disabled = false
      btn.textContent = '✓ Confirm & Execute'
    }
  }

  async #onDismissProposal() {
    if (!confirm('Dismiss this proposal? No orders will be placed.')) return
    try {
      await api.autopilotDismiss()
      alert('Proposal dismissed.')
      this.#load() // Reload to fall through to live orders
    } catch (err) {
      alert(`Dismiss failed: ${err.message}`)
    }
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
