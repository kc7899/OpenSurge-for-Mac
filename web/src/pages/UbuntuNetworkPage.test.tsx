// @vitest-environment jsdom
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { UbuntuNetworkPage } from './UbuntuNetworkPage'
import { api } from '../api'
import type { ControlConfig, Overview } from '../types'

vi.mock('../api', () => ({ api: { config: vi.fn(), saveConfig: vi.fn(), gateway: vi.fn() }, waitForOperation: vi.fn() }))
const config = { revision: 'one', gateway: { mode: 'same_lan', interface: 'ens18', upstream_interface: 'ens18', lan_ip: '192.168.8.20', lan_prefix_len: 24 }, dns: { listen: '192.168.8.20' }, mihomo: { store_fake_ip: true, sniffer_enabled: false }, local_system_dns: { enabled: true } } as ControlConfig
afterEach(() => { cleanup(); vi.clearAllMocks() })
describe('Ubuntu network controls', () => {
  it('saves a revisioned draft without starting or reloading the gateway', async () => {
    vi.mocked(api.config).mockResolvedValue(config)
    vi.mocked(api.saveConfig).mockImplementation(async c => ({ ...c, revision: 'two' }))
    render(<UbuntuNetworkPage overview={{ status: { gateway: 'stopped' } } as Overview} onChanged={vi.fn(async () => {})} />)
    await screen.findByDisplayValue('ens18')
    fireEvent.change(screen.getByDisplayValue('192.168.8.20'), { target: { value: '192.168.8.21' } })
    fireEvent.click(screen.getByText('保存设置'))
    await waitFor(() => expect(api.saveConfig).toHaveBeenCalled())
    expect(vi.mocked(api.saveConfig).mock.calls[0][0]).toMatchObject({ revision: 'one', gateway: { lan_ip: '192.168.8.21' }, dns: { listen: '192.168.8.21' } })
    expect(api.gateway).not.toHaveBeenCalled()
    expect(screen.queryByText('局域网 DHCP 接管')).toBeNull()
    expect(screen.queryByText('接管 Mac 本机 IPv6')).toBeNull()
  })
  it('blocks edits when runtime status is unknown', async () => {
    vi.mocked(api.config).mockResolvedValue(config)
    render(<UbuntuNetworkPage overview={null} onChanged={vi.fn(async () => {})} />)
    const input = await screen.findByDisplayValue('ens18')
    expect(input.closest('fieldset')?.disabled).toBe(true)
    expect((screen.getByText('启动网关') as HTMLButtonElement).disabled).toBe(true)
  })
})
