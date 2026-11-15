import { useEffect, useRef, useState } from 'react'
import { request } from '../api'
import { t } from '../i18n'

type Policy = { enabled: boolean; source_order: string[] | null; regions: string[]; forbidden: string[]; health_seconds: number; quota_seconds: number; cooldown_seconds: number }
type View = { policy: Policy; revision: string; message: string; sources: { id: string; name: string }[]; state: { mode: string; message: string; node?: string; country?: string; ip?: string; reason?: string; updated_at?: string; quotas?: Record<string, {known: boolean; used: number; total: number; error?: string}> } }
export function StableRoutingCard({ onSaved }: { onSaved?: () => void } = {}) {
 const [view, setView] = useState<View | null>(null)
 const [draft, setDraft] = useState<Policy | null>(null)
 const [revision, setRevision] = useState('')
 const [error, setError] = useState('')
 const [busy, setBusy] = useState(false)
 const dirty = useRef(false)
 const refresh = async () => { try { const v = await request<View>('/api/v1/stable-routing'); setView(v); if (!dirty.current) { setDraft(v.policy); setRevision(v.revision) }; setError('') } catch (e) { setError(String(e)) } }
 useEffect(() => { void refresh(); const timer = setInterval(() => { if (document.visibilityState === 'visible') void refresh() }, 10000); return () => clearInterval(timer) }, [])
 const edit = (next: Policy) => { dirty.current = true; setDraft(next) }
 const save = async () => { if (!draft) return; setBusy(true); try { const v = await request<View>('/api/v1/stable-routing', { method: 'PUT', headers: { 'If-Match': revision }, body: JSON.stringify(draft) }); dirty.current = false; setView(v); setDraft(v.policy); setRevision(v.revision); setError(''); onSaved?.() } catch (e) { setError(String(e)) } finally { setBusy(false) } }
 const check = async () => { setBusy(true); try { await request('/api/v1/stable-routing/check', { method: 'POST', body: '{}' }); await refresh() } catch (e) { setError(String(e)) } finally { setBusy(false) } }
 const ids = draft?.source_order?.length ? [...draft.source_order, ...(view?.sources ?? []).filter(s => !draft.source_order?.includes(s.id)).map(s => s.id)] : (view?.sources ?? []).map(s => s.id)
 const move = (index: number, by: number) => { if (!draft) return; const order = [...ids]; [order[index], order[index + by]] = [order[index + by], order[index]]; edit({ ...draft, source_order: order }) }
 return <section className="card" id="stable-routing">
  <h2>{t('稳定优先自动切换')}</h2>
  {error && <p role="alert">{error}</p>}
  {draft && <>
   <label><input type="checkbox" checked={draft.enabled} onChange={e => edit({ ...draft, enabled: e.target.checked })} />{t('启用自动切换')}</label>
   <p>{t('当前线路可用就保持；确认故障、额度耗尽或地区不合规时才切换。')}</p>
   <div className="form-grid">
    {([['health_seconds', '线路检测间隔（秒）'], ['quota_seconds', '额度检测间隔（秒）'], ['cooldown_seconds', '故障冷却（秒）']] as const).map(([key, label]) => <label key={key}>{t(label)}<input type="number" value={draft[key]} min={key === 'health_seconds' ? 15 : key === 'cooldown_seconds' ? 30 : 60} onChange={e => edit({ ...draft, [key]: Number(e.target.value) })} /></label>)}
    <label>{t('地区顺序（国家代码，逗号分隔）')}<input value={draft.regions.join(',')} onChange={e => edit({ ...draft, regions: e.target.value.toUpperCase().split(',').map(s => s.trim()) })} /></label>
    <label>{t('禁止地区（国家代码，逗号分隔）')}<input value={draft.forbidden.join(',')} onChange={e => edit({ ...draft, forbidden: e.target.value.toUpperCase().split(',').map(s => s.trim()).filter(Boolean) })} /></label>
   </div>
   <h3>{t('订阅使用顺序')}</h3>
   <ol>{ids.map((id, index) => <li key={id}>{view?.sources.find(s => s.id === id)?.name ?? id} <button disabled={busy || index === 0} onClick={() => move(index, -1)} aria-label={t('上移')}>↑</button> <button disabled={busy || index === ids.length - 1} onClick={() => move(index, 1)} aria-label={t('下移')}>↓</button></li>)}</ol>
   <div className="button-row"><button disabled={busy} onClick={() => void save()}>{t('保存设置')}</button><button disabled={busy || !draft.enabled || dirty.current} onClick={() => void check()}>{t('立即检查')}</button></div>
  </>}
  {view && <><p>{view.message}</p><p>{view.state.message}</p><p>{t('当前线路')}：{view.state.node || '—'} · {view.state.country || '—'} · {view.state.ip || '—'}</p><p>{t('切换原因')}：{view.state.reason || '—'}</p>
   {view.sources.map(source => { const q = view.state.quotas?.[source.id]; return <p key={source.id}>{source.name}：{q?.known ? `${Math.max(0, q.total - q.used).toLocaleString()} / ${q.total.toLocaleString()} B` : t('额度未知')}{q?.error ? ` · ${q.error}` : ''}</p> })}
  </>}
 </section>
}
