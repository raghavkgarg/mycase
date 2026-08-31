import './components/portfolio-header.js'
import './components/holdings-table.js'
import './components/weight-donut.js'
import './components/weight-comparison.js'
import './components/equity-curve.js'
import './components/drawdown-chart.js'
import './components/metrics-grid.js'
import './components/backtest-form.js'
import './components/order-preview.js'
import './components/tax-warnings.js'
import './components/tax-tab.js'
import './components/performance-tab.js'
import './components/monitor-table.js'
import './components/drift-timeline.js'

let currentPortfolio = null
let sseSource = null

const VIEWS = ['dashboard', 'backtest', 'rebalance', 'monitor', 'drift', 'tax', 'performance']

const routes = {
  '#/':          'dashboard',
  '#/backtest':  'backtest',
  '#/rebalance': 'rebalance',
  '#/monitor':   'monitor',
  '#/drift':     'drift',
  '#/tax':       'tax',
  '#/performance': 'performance',
}

function showView(name) {
  for (const v of VIEWS) {
    const el = document.getElementById(`view-${v}`)
    if (el) el.classList.toggle('active', v === name)
  }
  // Update active tab link
  for (const [hash, view] of Object.entries(routes)) {
    const tabId = `tab-${view}`
    const tab = document.getElementById(tabId)
    if (tab) tab.classList.toggle('active', view === name)
  }
}

function initSSE() {
  if (sseSource) sseSource.close()
  sseSource = new EventSource('/api/quotes')
  sseSource.onmessage = (e) => {
    try {
      const quotes = JSON.parse(e.data)
      document.dispatchEvent(new CustomEvent('quote-update', { detail: quotes }))
    } catch (_) {}
  }
  sseSource.onerror = () => {
    // Will auto-reconnect
  }
}

document.addEventListener('portfolio-changed', (e) => {
  currentPortfolio = e.detail
  // Update portfolio attribute on all registered components
  const componentTags = [
    'holdings-table', 'weight-donut', 'weight-comparison',
    'backtest-form', 'order-preview', 'monitor-table', 'drift-timeline',
    'equity-curve', 'drawdown-chart', 'metrics-grid', 'tax-warnings', 'tax-tab', 'performance-tab',
  ]
  for (const tag of componentTags) {
    const el = document.querySelector(tag)
    if (el) el.setAttribute('portfolio', currentPortfolio)
  }
  initSSE()
  document.dispatchEvent(new CustomEvent('portfolio-reload', { detail: currentPortfolio }))
})

window.addEventListener('hashchange', () => {
  const view = routes[location.hash]
  if (view) showView(view)
})

// Init on load
const initView = routes[location.hash] || routes['#/']
showView(initView)
