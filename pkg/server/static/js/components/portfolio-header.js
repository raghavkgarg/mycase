import { api } from '../api.js'

class PortfolioHeader extends HTMLElement {
  #select = null

  connectedCallback() {
    this.#render()
    this.#load()
  }

  disconnectedCallback() {
    if (this.#select) {
      this.#select.removeEventListener('change', this.#onChange)
    }
  }

  #render() {
    this.innerHTML = `<select id="portfolio-select"><option value="">Loading…</option></select>`
    this.#select = this.querySelector('select')
    this.#select.addEventListener('change', this.#onChange)
  }

  async #load() {
    try {
      const names = await api.portfolios()
      if (!names || names.length === 0) {
        this.#select.innerHTML = '<option value="">No portfolios found</option>'
        return
      }
      this.#select.innerHTML = names.map(n =>
        `<option value="${n}">${n}</option>`
      ).join('')
      // Fire initial selection
      const first = names[0]
      this.setAttribute('portfolio', first)
      document.dispatchEvent(new CustomEvent('portfolio-changed', { detail: first }))
    } catch (err) {
      this.#select.innerHTML = '<option value="">Error loading</option>'
    }
  }

  #onChange = () => {
    const val = this.#select.value
    if (!val) return
    this.setAttribute('portfolio', val)
    document.dispatchEvent(new CustomEvent('portfolio-changed', { detail: val }))
  }
}

customElements.define('portfolio-header', PortfolioHeader)
