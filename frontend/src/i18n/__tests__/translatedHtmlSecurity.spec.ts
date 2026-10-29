import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'

function translateHtml(message: string): string {
  // Message functions exercise the runtime build without a string compiler.
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    escapeParameter: true,
    warnHtmlMessage: false,
    messages: { en: { message: () => message } }
  })

  try {
    return i18n.global.t('message')
  } finally {
    i18n.dispose()
  }
}

describe('translated HTML security', () => {
  // GHSA-83rm-6c8r-g728: escaping srcdoc's value alone still creates a document.
  it.each([
    '<iframe srcdoc="<b>x</b>"></iframe>',
    '<iframe SrcDoc="&lt;b&gt;x&lt;/b&gt;"></iframe>'
  ])('neutralizes the srcdoc attribute in %s', (message) => {
    const template = document.createElement('template')
    template.innerHTML = translateHtml(message)

    const iframe = template.content.querySelector('iframe')
    expect(iframe).not.toBeNull()
    expect(iframe?.hasAttribute('srcdoc')).toBe(false)
  })

  it('preserves ordinary translated HTML and harmless srcdoc text', () => {
    const template = document.createElement('template')
    template.innerHTML = translateHtml('<span title="srcdoc=x">use srcdoc=x</span>')

    const span = template.content.querySelector('span')
    expect(span?.getAttribute('title')).toBe('srcdoc=x')
    expect(span?.textContent).toBe('use srcdoc=x')
  })
})
