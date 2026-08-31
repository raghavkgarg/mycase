export const api = {
  portfolios: () => fetch('/api/portfolios').then(r => r.json()),
  weights: (name) => fetch(`/api/portfolio/${name}/weights`).then(r => r.json()),
  holdings: (name) => fetch(`/api/portfolio/${name}/holdings`).then(r => r.json()),
  drift: (name) => fetch(`/api/portfolio/${name}/drift`).then(r => r.json()),
  orders: (name, freshCash = 0) => fetch(`/api/portfolio/${name}/orders?fresh_cash=${freshCash}`).then(r => r.json()),
  monitor: (name, style = 'moderate') => fetch(`/api/portfolio/${name}/monitor?style=${style}`).then(r => r.json()),
  tax: (name) => fetch(`/api/portfolio/${name}/tax`).then(r => r.json()),
  performance: (name) => fetch(`/api/portfolio/${name}/performance`).then(r => r.json()),
  cacheStatus: () => fetch('/api/cache/status').then(r => r.json()),
  daemonHistory: () => fetch('/api/daemon/history').then(r => r.json()),

  // Streams NDJSON; calls onSnapshot(snap) per line, returns final result object.
  backtest: async (name, params, onSnapshot) => {
    const resp = await fetch(`/api/portfolio/${name}/backtest`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(params),
    })
    if (!resp.ok) {
      const text = await resp.text()
      throw new Error(text || `HTTP ${resp.status}`)
    }
    const reader = resp.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''
    let result = null
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      const lines = buf.split('\n')
      buf = lines.pop()
      for (const line of lines) {
        if (!line.trim()) continue
        try {
          const obj = JSON.parse(line)
          if (obj.type === 'snapshot') onSnapshot(obj)
          else if (obj.type === 'result') result = obj
        } catch (_) { /* skip malformed line */ }
      }
    }
    return result
  },

  execute: (name) => fetch(`/api/portfolio/${name}/execute`, { method: 'POST' }).then(r => r.json()),

  // Autopilot proposal endpoints
  autopilotProposal: () => fetch('/api/autopilot/proposal').then(r => r.json()),
  autopilotConfirm: () => fetch('/api/autopilot/confirm', { method: 'POST' }).then(r => r.json()),
  autopilotDismiss: () => fetch('/api/autopilot/dismiss', { method: 'POST' }).then(r => r.json()),
}

// Format a number as Indian currency string: ₹X,XX,XXX.XX
export function formatINR(value) {
  if (value == null || isNaN(value)) return '—'
  const abs = Math.abs(value)
  const sign = value < 0 ? '-' : ''
  // Indian numbering: last 3 digits, then groups of 2
  const str = abs.toFixed(2)
  const [intPart, decPart] = str.split('.')
  let formatted = ''
  const len = intPart.length
  if (len <= 3) {
    formatted = intPart
  } else {
    const last3 = intPart.slice(-3)
    const rest = intPart.slice(0, len - 3)
    const groups = []
    let i = rest.length
    while (i > 0) {
      groups.unshift(rest.slice(Math.max(0, i - 2), i))
      i -= 2
    }
    formatted = groups.join(',') + ',' + last3
  }
  return `${sign}₹${formatted}.${decPart}`
}

// Format a percentage with sign and 2 decimal places
export function formatPct(value) {
  if (value == null || isNaN(value)) return '—'
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(2)}%`
}

// Format a number as US currency string: $X,XXX.XX
export function formatUSD(value) {
  if (value == null || isNaN(value)) return '—'
  const sign = value < 0 ? '-' : ''
  const abs = Math.abs(value)
  return `${sign}$${abs.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}
