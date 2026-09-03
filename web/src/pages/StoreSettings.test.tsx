import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import StoreSettings from './StoreSettings'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    if (url === '/api/store' && !init?.method) {
      return json({
        store: {
          id: 's1',
          gst_registration_id: null,
          name: 'Kadam Medicals',
          address: 'Shop 3, Camp, Pune',
          phone: '02026214455',
          drug_license_number: 'MH/DRG/2020/12345',
          drug_license_expiry: '2030-12-31',
          is_active: true,
          max_employees: 5,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
          owner_name: 'R. Kadam',
          gstin: '27AAPBC1234F1ZV',
          pan: 'AABBC1234D',
        },
      })
    }
    if (url === '/api/employees') {
      return json({ members: [] })
    }
    if (url === '/api/store' && init?.method === 'PUT') {
      return json({ store: JSON.parse(String(init.body)) })
    }
    return json({}, 404)
  }))
})

describe('StoreSettings', () => {
  it('renders all shop fields', async () => {
    render(<StoreSettings />)
    expect(await screen.findByDisplayValue('Kadam Medicals')).toBeInTheDocument()
    expect(screen.getByDisplayValue('R. Kadam')).toBeInTheDocument()
    expect(screen.getByDisplayValue('02026214455')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Shop 3, Camp, Pune')).toBeInTheDocument()
    expect(screen.getByDisplayValue('27AAPBC1234F1ZV')).toBeInTheDocument()
    expect(screen.getByDisplayValue('MH/DRG/2020/12345')).toBeInTheDocument()
    expect(screen.getByDisplayValue('2030-12-31')).toBeInTheDocument()
    expect(screen.getByDisplayValue('AABBC1234D')).toBeInTheDocument()
    expect(screen.getByDisplayValue('5')).toBeInTheDocument()
  })

  it('posts the full body on save and shows success only after the server returns', async () => {
    render(<StoreSettings />)
    await screen.findByDisplayValue('Kadam Medicals')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /Save changes/i }))

    await waitFor(() => expect(screen.getByText('Settings saved.')).toBeInTheDocument())

    const putCall = vi.mocked(fetch).mock.calls.find(
      (c) => String(c[0]) === '/api/store' && c[1]?.method === 'PUT',
    )
    expect(putCall).toBeTruthy()
    const body = JSON.parse(String(putCall![1]!.body))
    expect(body).toEqual({
      name: 'Kadam Medicals',
      address: 'Shop 3, Camp, Pune',
      phone: '02026214455',
      owner_name: 'R. Kadam',
      max_employees: 5,
      gstin: '27AAPBC1234F1ZV',
      pan: 'AABBC1234D',
      drug_license_number: 'MH/DRG/2020/12345',
      drug_license_expiry: '2030-12-31',
    })
  })
})
