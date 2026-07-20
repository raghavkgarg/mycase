import { api } from '../api.js'

class BacktestForm extends HTMLElement {
  static observedAttributes = ['portfolio']

  #portfolio = null
  #running = false

  connectedCallback() {
    this.#render()
    if (this.hasAttribute('portfolio')) {
      this.#portfolio = this.getAttribute('portfolio')
    }
  }

  attributeChangedCallback(name, _old, val) {
    if (name === 'portfolio') this.#portfolio = val
  }

  #render() {
    // Default from date: 2 years ago
    const twoYearsAgo = new Date()
    twoYearsAgo.setFullYear(twoYearsAgo.getFullYear() - 2)
    const fromDefault = twoYearsAgo.toISOString().slice(0, 10)
    const toDefault = new Date().toISOString().slice(0, 10)

    this.innerHTML = `
      <div class="card">
        <p class="card-title">Backtest Parameters</p>
        <form id="backtest-form-inner">
          <div class="form-row">
            <div class="form-group">
              <label>From</label>
              <input type="date" name="from" value="${fromDefault}" required />
            </div>
            <div class="form-group">
              <label>To</label>
              <input type="date" name="to" value="${toDefault}" />
            </div>
            <div class="form-group">
              <label>Rebalance</label>
              <select name="rebalance">
                <option value="quarterly">Quarterly</option>
                <option value="monthly">Monthly</option>
                <option value="drift-triggered">Drift-triggered</option>
              </select>
            </div>
            <div class="form-group">
              <label>Capital (₹)</label>
              <input type="number" name="capital" value="100000" min="1000" />
            </div>
            <div class="form-group">
              <label>Slippage %</label>
              <input type="number" name="slippage" value="0.1" step="0.05" min="0" />
            </div>
            <div class="form-group">
              <label>Benchmark</label>
              <select name="benchmark">
                <option value="^NSEI">Nifty 50</option>
                <option value="^CNXSC">Nifty SmallCap</option>
                <option value="^CNXMID">Nifty MidCap</option>
              </select>
            </div>
            <div class="form-group" style="justify-content:flex-end">
              <button type="submit" class="btn btn-primary" id="bt-run-btn">Run Backtest</button>
            </div>
          </div>
          <div class="progress-hint" id="bt-progress"></div>
        </form>
      </div>`

    this.querySelector('#backtest-form-inner').addEventListener('submit', this.#onSubmit)
  }

  #onSubmit = async (e) => {
    e.preventDefault()
    if (this.#running || !this.#portfolio) return
    this.#running = true
    const btn = this.querySelector('#bt-run-btn')
    const progress = this.querySelector('#bt-progress')
    btn.disabled = true
    btn.textContent = 'Running…'
    progress.textContent = ''

    document.dispatchEvent(new CustomEvent('backtest-reset'))

    const form = e.target
    const params = {
      from: form.from.value,
      to: form.to.value,
      rebalance: form.rebalance.value,
      capital: parseFloat(form.capital.value) || 100000,
      slippage: parseFloat(form.slippage.value) || 0.1,
      benchmark: form.benchmark.value,
    }

    let snapshotCount = 0
    try {
      const result = await api.backtest(this.#portfolio, params, (snap) => {
        snapshotCount++
        document.dispatchEvent(new CustomEvent('backtest-snapshot', { detail: snap }))
        if (snapshotCount % 20 === 0) {
          progress.textContent = `${snapshotCount} trading days processed…`
        }
      })
      if (result) {
        progress.textContent = `Done — ${snapshotCount} trading days.`
        document.dispatchEvent(new CustomEvent('backtest-done', { detail: result }))
      }
    } catch (err) {
      progress.textContent = `Error: ${err.message}`
    } finally {
      this.#running = false
      btn.disabled = false
      btn.textContent = 'Run Backtest'
    }
  }
}

customElements.define('backtest-form', BacktestForm)
