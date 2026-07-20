class DrawdownChart extends HTMLElement {
  #chart = null
  #dates = []
  #portfolio = []
  #snapshotListener = null
  #doneListener = null

  connectedCallback() {
    this.innerHTML = `
      <p class="card-title">Drawdown</p>
      <div class="chart-container" style="height:180px"></div>`
    this.#snapshotListener = (e) => this.#onSnapshot(e.detail)
    this.#doneListener = () => this.#render()
    document.addEventListener('backtest-snapshot', this.#snapshotListener)
    document.addEventListener('backtest-reset', this.#reset.bind(this))
    document.addEventListener('backtest-done', this.#doneListener)
  }

  disconnectedCallback() {
    document.removeEventListener('backtest-snapshot', this.#snapshotListener)
    document.removeEventListener('backtest-done', this.#doneListener)
    document.removeEventListener('backtest-reset', this.#reset.bind(this))
    this.#chart?.dispose()
    this.#chart = null
  }

  #reset() {
    this.#dates = []
    this.#portfolio = []
  }

  #onSnapshot(snap) {
    this.#dates.push(snap.date)
    this.#portfolio.push(snap.portfolio)
  }

  #render() {
    if (!window.echarts) return
    const container = this.querySelector('.chart-container')
    if (!container) return

    // Compute drawdown series
    let peak = this.#portfolio[0] || 0
    const drawdown = this.#portfolio.map(v => {
      if (v > peak) peak = v
      return peak > 0 ? +((v / peak - 1) * 100).toFixed(2) : 0
    })

    if (!this.#chart) this.#chart = window.echarts.init(container, 'dark')
    this.#chart.setOption({
      backgroundColor: 'transparent',
      tooltip: { trigger: 'axis', formatter: (params) => `${params[0].name}<br/>Drawdown: ${params[0].value}%` },
      grid: { left: 50, right: 20, top: 10, bottom: 30 },
      xAxis: { type: 'category', data: this.#dates, axisLabel: { color: '#8892a4', fontSize: 10, interval: 'auto' } },
      yAxis: { type: 'value', axisLabel: { formatter: '{value}%', color: '#8892a4' }, max: 0 },
      series: [{
        type: 'line',
        data: drawdown,
        symbol: 'none',
        lineStyle: { color: '#e05252', width: 1 },
        areaStyle: { color: 'rgba(224,82,82,0.15)' },
      }],
    })
  }
}

customElements.define('drawdown-chart', DrawdownChart)
