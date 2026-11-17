// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { StableRoutingCard } from './StableRoutingCard'
const { request } = vi.hoisted(() => ({ request: vi.fn() }))
vi.mock('../api', () => ({ request }))
afterEach(() => { cleanup(); request.mockReset() })
it('shows certificate evidence and saves the guard switch without applying or selecting an outlet', async () => {
 const view = { policy: {enabled:true,chatgpt_tls:true,source_order:[],regions:['US','SG','JP'],forbidden:['HK'],health_seconds:60,quota_seconds:600,cooldown_seconds:300}, revision:'revision', message:'设置已保存', sources:[], state:{mode:'active',message:'保持当前线路',reason:'service_certificate_no_verified_backup',service_tls:{status:'certificate_failed',host:'chatgpt.com',error:'hostname_mismatch',checked_at:'2026-11-17T01:00:00Z'}} }
 request.mockImplementation(async (_path, init) => init?.method === 'PUT' ? { ...view, policy:JSON.parse(init.body) } : view)
 render(<StableRoutingCard />)
 const toggle = await screen.findByRole('checkbox', { name:'ChatGPT 证书异常确认后自动切换全网出口' })
 expect((toggle as HTMLInputElement).checked).toBe(true)
 expect(screen.getByText(/ChatGPT 证书异常，暂无验证通过的备用/)).toBeTruthy()
 expect(screen.getByText(/证书域名不匹配/)).toBeTruthy()
 fireEvent.click(toggle)
 fireEvent.click(screen.getByRole('button',{name:'保存设置'}))
 await waitFor(() => expect(request).toHaveBeenCalledWith('/api/v1/stable-routing',expect.objectContaining({method:'PUT',body:expect.stringContaining('"chatgpt_tls":false')})))
 expect(request.mock.calls.every(([path])=>path==='/api/v1/stable-routing')).toBe(true)
})
