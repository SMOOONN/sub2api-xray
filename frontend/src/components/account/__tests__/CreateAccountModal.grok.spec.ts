import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

describe('CreateAccountModal Grok account types', () => {
  it('offers API-key setup alongside OAuth with the official xAI default', () => {
    expect(source).toContain('data-testid="grok-account-type-api-key"')
    expect(source).toContain("@click=\"accountCategory = 'apikey'\"")
    expect(source).toContain("newPlatform === 'grok'")
    expect(source).toContain("? 'https://api.x.ai/v1'")
    expect(source).toContain("form.platform === 'grok'")
    expect(source).toContain("? 'xai-...'")
  })

  it('exposes custom upstream URL and header override for the OAuth create flow', () => {
    expect(source).toContain('data-testid="grok-custom-base-url-toggle"')
    expect(source).toContain('data-testid="grok-custom-base-url-input"')
    expect(source).toContain('form.platform === \'grok\' && isOAuthFlow')
  })

  it('validates and applies upstream config on every Grok OAuth create path', () => {
    // Authorization-code, refresh-token batch, SSO batch, and the official
    // single-account continuation paths all validate the upstream config.
    expect(source.match(/validateGrokOAuthUpstreamConfig\(\)/g)?.length).toBe(5)
    expect(source.match(/applyGrokOAuthUpstreamConfig\(credentials\)/g)?.length).toBe(5)
  })

  it('hides Grok password authorize option in the create flow', () => {
    expect(source).toContain(':show-email-password-option="false"')
  })
})
