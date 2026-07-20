class EquityCurve extends HTMLElement {
  #chart = null
  #dates = []
  #portfolio = []
  #benchmark = []
  #snapshotListener = null
  #doneListener = null

  connectedCallback() {
    this.innerHTML = `
      <p class="card-title">Equity Curve</p>
      <div class="chart-container tall"></div>`
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
    this.#benchmark = []
    const container = this.querySelector('.chart-container')
    if (container) container.innerHTML = ''
  }

  #onSnapshot(snap) {
    this.#dates.push(snap.date)
    this.#portfolio.push(snap.portfolio)
    this.#benchmark.push(snap.benchmark)
  }

  #render() {
    if (!window.echarts) return
    const container = this.querySelector('.chart-container')
    if (!container) return

    const base = this.#portfolio[0] || 1
    const benchBase = this.#benchmark[0] || 1
    const portNorm = this.#portfolio.map(v => +((v / base * 100).toFixed(2)))
    const benchNorm = this.#benchmark.map(v => +((v / benchBase * 100).toFixed(2)))

    if (!this.#chart) this.#chart = window.echarts.init(container, 'dark')
    this.#chart.setOption({
      backgroundColor: 'transparent',
      tooltip: { trigger: 'axis' },
      legend: { data: ['Portfolio', 'Benchmark'], textStyle: { color: '#8892a4' } },
      grid: { left: 50, right: 20, top: 40, bottom: 30 },
      xAxis: { type: 'category', data: this.#dates, axisLabel: { color: '#8892a4', fontSize: 10, interval: 'auto' } },
      yAxis: { type: 'value', axisLabel: { formatter: '{value}', color: '#8892a4' } },
      series: [
        { name: 'Portfolio', type: 'line', data: portNorm, smooth: false, symbol: 'none', lineStyle: { color: '#4f8ef7', width: 2 }, areaStyle: { color: 'rgba(79,142,247,0.08)' } },
        { name: 'Benchmark', type: 'line', data: benchNorm, smooth: false, symbol: 'none', lineStyle: { color: '#8892a4', width: 1.5, type: 'dashed' } },
      ],
    })
  }
}

customElements.define('equity-curve', EquityCurve)
