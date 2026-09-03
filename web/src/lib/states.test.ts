import { describe, expect, it } from 'vitest'
import {
  INDIAN_STATES,
  isValidGSTINShape,
  stateCodeToName,
  stateNameToCode,
} from './states'

describe('GST state codes', () => {
  it('maps the key states to their official two-digit codes', () => {
    expect(stateNameToCode('Maharashtra')).toBe('27')
    expect(stateNameToCode('Delhi')).toBe('07')
    expect(stateNameToCode('Assam')).toBe('18')
    expect(stateNameToCode('Karnataka')).toBe('29')
    expect(stateNameToCode('Andhra Pradesh')).toBe('37')
    expect(stateNameToCode('Telangana')).toBe('36')
  })

  it('drops the discontinued codes 25 and 28 and keeps 26 + 97', () => {
    const codes = INDIAN_STATES.map((s) => s.code)
    expect(codes).not.toContain('25')
    expect(codes).not.toContain('28')
    expect(codes).toContain('26')
    expect(codes).toContain('97')
    expect(stateCodeToName('26')).toBe('Dadra & Nagar Haveli and Daman & Diu')
    expect(stateCodeToName('97')).toBe('Other Territory')
  })

  it('round-trips code and name for every entry', () => {
    for (const s of INDIAN_STATES) {
      expect(stateCodeToName(s.code)).toBe(s.name)
      expect(stateNameToCode(s.name)).toBe(s.code)
    }
  })

  it('validates GSTIN shape including the state-code prefix', () => {
    expect(isValidGSTINShape('27AAPBC1234F1ZV')).toBe(true)
    expect(isValidGSTINShape('29AAPBC1234F1ZR')).toBe(true)
    expect(isValidGSTINShape('27AAAAA1111A1ZW')).toBe(true)
    expect(isValidGSTINShape('07AAPBC1234F1Z5')).toBe(true)
    expect(isValidGSTINShape('99AAAAA0000A1Z5')).toBe(false)
    expect(isValidGSTINShape('27AABCU9603R1ZM')).toBe(true)
    expect(isValidGSTINShape('27-SHORT-GSTIN')).toBe(false)
  })
})