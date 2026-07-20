import { api } from '../api.js'

class WeightDonut extends HTMLElement {
  static observedAttributes = ['portfolio']

  #portfolio = null
  #chart = null

  connectedCallback() {
    this.innerHTML = `
      <div class="card">
        <p class="card-title">Portfolio Weights</p>
        <div class="chart-container" id="donut-chart-${this.#uid()}"></div>
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
      const data = await api.holdings(this.#portfolio)
      const { holdings = [] } = data
      if (!window.echarts) return
      const container = this.querySelector('.chart-container')
      if (!container) return
      if (!this.#chart) {
        this.#chart = window.echarts.init(container, 'dark')
      }
      const seriesData = holdings
        .filter(h => h.actual_weight > 0)
        .map(h => ({ name: h.ticker, value: +(h.actual_weight * 100).toFixed(2) }))
      this.#chart.setOption({
        backgroundColor: 'transparent',
        tooltip: { trigger: 'item', formatter: '{b}: {c}%' },
        series: [{
          type: 'pie',
          radius: ['40%', '70%'],
          data: seriesData,
          label: { color: '#8892a4', fontSize: 11 },
          itemStyle: { borderColor: '#1a1d24', borderWidth: 2 },
        }],
      })
    } catch (_) {}
  }

  #uid() {
    return Math.random().toString(36).slice(2, 8)
  }
}

customElements.define('weight-donut', WeightDonut)
