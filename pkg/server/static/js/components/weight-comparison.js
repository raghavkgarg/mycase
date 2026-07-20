import { api } from '../api.js'

class WeightComparison extends HTMLElement {
  static observedAttributes = ['portfolio']

  #portfolio = null
  #chart = null

  connectedCallback() {
    this.innerHTML = `
      <div class="card">
        <p class="card-title">Actual vs Target Weights</p>
        <div class="chart-container" style="height:360px"></div>
      </div>`
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

  async #load() {
    if (!this.#portfolio) return
    try {
      const [holdData, wtData] = await Promise.all([
        api.holdings(this.#portfolio),
        api.weights(this.#portfolio),
      ])
      const { holdings = [] } = holdData
      const { weights = {} } = wtData

      // Sort by abs(actual - target) descending
      const rows = holdings.map(h => ({
        ticker: h.ticker,
        actual: +(h.actual_weight * 100).toFixed(2),
        target: +((weights[`${h.exchange}:${h.ticker}`] || 0) * 100).toFixed(2),
      })).sort((a, b) => Math.abs(b.actual - b.target) - Math.abs(a.actual - a.target))

      if (!window.echarts) return
      const container = this.querySelector('.chart-container')
      if (!container) return
      if (!this.#chart) {
        this.#chart = window.echarts.init(container, 'dark')
      }

      this.#chart.setOption({
        backgroundColor: 'transparent',
        tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
        legend: { data: ['Actual', 'Target'], textStyle: { color: '#8892a4' } },
        grid: { left: 80, right: 20, top: 40, bottom: 20 },
        xAxis: { type: 'value', axisLabel: { formatter: '{value}%', color: '#8892a4' } },
        yAxis: { type: 'category', data: rows.map(r => r.ticker), axisLabel: { color: '#8892a4', fontSize: 11 } },
        series: [
          { name: 'Actual', type: 'bar', data: rows.map(r => r.actual), itemStyle: { color: '#4f8ef7' } },
          { name: 'Target', type: 'bar', data: rows.map(r => r.target), itemStyle: { color: '#2d3139' } },
        ],
      })
    } catch (_) {}
  }
}

customElements.define('weight-comparison', WeightComparison)
