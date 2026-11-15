// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { PolicyGroupHealthCard } from './PolicyGroupHealthCard'
import type { ProxyHealthEntry } from '../types'
afterEach(cleanup)
it('shows original node country/name and selects by the unchanged internal ID', async () => {
 const id = 'stable-internal-identity'
 const onSelect = vi.fn(async () => {})
 const health: ProxyHealthEntry = { name: id, display_name: '🇺🇸 美国-US-1 · 订阅2', type: 'Trojan', selected: '', provider: '', udp: true, status: 'reachable', probeable: true }
 render(<PolicyGroupHealthCard group={{ name: 'open-surge/stable-routing', type: 'Selector', selected: 'REJECT', options: [id] }} search="美国" healthByName={new Map([[id, health]])} testing={new Set()} onTest={async () => {}} onSelect={onSelect} />)
 expect(screen.getByText('🇺🇸 美国-US-1 · 订阅2')).toBeTruthy()
 expect(screen.queryByText(id)).toBeNull()
 fireEvent.click(screen.getByRole('button', { name: /选择.*美国-US-1/ }))
 await waitFor(() => expect(onSelect).toHaveBeenCalledWith(id))
})
