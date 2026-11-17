import { useEffect, useState } from 'react'
import { api, waitForOperation } from '../api'
import type { ControlConfig, Overview } from '../types'
import { PageHeader } from '../components/Common'
import { t } from '../i18n'

// Uses the same revisioned configuration API and serialized lifecycle as the
// Mac view; only supported Linux network controls are presented here.
export function UbuntuNetworkPage({ overview, onChanged }: { overview: Overview | null; onChanged: () => Promise<void> }) {
  const [config, setConfig] = useState<ControlConfig | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const running = overview?.status.gateway === 'running'
  const stopped = overview?.status.gateway === 'stopped'
  useEffect(() => { void api.config().then(setConfig).catch(e => setError(String(e))) }, [])
  const save = async () => {
    if (!config) return
    setBusy(true); setError(''); setSaved(false)
    try { setConfig(await api.saveConfig(config)); setSaved(true); await onChanged() }
    catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy(false) }
  }
  const lifecycle = async (action: 'start' | 'stop') => {
    if (action === 'stop' && !window.confirm(t('停止旁路由后，仍使用它作为网关或 DNS 的设备会断网。确定停止吗？'))) return
    setBusy(true); setError('')
    try { const operation = await api.gateway(action); await waitForOperation(operation.id); await onChanged() }
    catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy(false) }
  }
  return <>
    <PageHeader eyebrow="UBUNTU SERVER" title="Ubuntu 旁路由" description="同一局域网 IPv4 网关。保留主路由 DHCP；客户端的网关和 DNS 都应指向本服务器。" />
    {error && <div className="notice warn" role="alert">{error}</div>}
    <div className="notice">{t('首次安装不会接管网络。请先为服务器保留固定 IPv4，再导入并应用订阅，最后启动网关。修改下方设置不会改写 Netplan。')}</div>
    {config && <section className="section">
      <h2>{t('服务器网络')}</h2>
      <fieldset disabled={busy || !stopped} style={{ border: 0 }}>
        <div className="config-form">
          <label className="config-field"><strong>{t('LAN 网络接口')}</strong><input value={config.gateway.interface} onChange={e => { setSaved(false); setConfig({ ...config, gateway: { ...config.gateway, interface: e.target.value, upstream_interface: e.target.value } }) }} /></label>
          <label className="config-field"><strong>{t('服务器 IPv4 / 客户端 DNS')}</strong><input value={config.gateway.lan_ip} onChange={e => { setSaved(false); setConfig({ ...config, gateway: { ...config.gateway, lan_ip: e.target.value }, dns: { ...config.dns, listen: e.target.value } }) }} /></label>
          <label className="config-field"><strong>{t('LAN 子网前缀')}</strong><input type="number" min="8" max="30" value={config.gateway.lan_prefix_len} onChange={e => { setSaved(false); setConfig({ ...config, gateway: { ...config.gateway, lan_prefix_len: Number(e.target.value) } }) }} /></label>
          <label><input type="checkbox" checked={config.local_system_dns?.enabled ?? true} onChange={e => { setSaved(false); setConfig({ ...config, local_system_dns: { enabled: e.target.checked } }) }} /> {t('随网关管理服务器 systemd-resolved DNS，并在停止时恢复')}</label>
          <label><input type="checkbox" checked={config.mihomo.store_fake_ip} onChange={e => { setSaved(false); setConfig({ ...config, mihomo: { ...config.mihomo, store_fake_ip: e.target.checked } }) }} /> {t('重启后保留 fake-IP 映射')}</label>
          <label><input type="checkbox" checked={config.mihomo.sniffer_enabled ?? false} onChange={e => { setSaved(false); setConfig({ ...config, mihomo: { ...config.mihomo, sniffer_enabled: e.target.checked } }) }} /> {t('启用 Sniffer')}</label>
        </div>
        <button disabled={busy} onClick={() => void save()}>{t('保存设置')}</button>
      </fieldset>
      {saved && <p role="status">{t('设置已保存；下次启动网关后生效。')}</p>}
      {!stopped && <p>{t('网关运行中或状态未确认时，不能修改网络设置。')}</p>}
      <div className="notice warn">{t('此预览版只接管 IPv4。通过 Ubuntu 转发的 IPv6 会被阻断，但设备仍可能通过主路由的 IPv6 绕过它。需在客户端移除该 IPv6 默认路由或关闭 IPv6。此版本不接管 DHCP、不修改 Tailnet ACL。')}</div>
      <p>{t('日常节点检测和线路切换不会重载网关。只有应用订阅结构或网络配置等明确操作才可能需要重新应用。')}</p>
      <button id="gateway-control" disabled={busy || (!running && !stopped)} onClick={() => void lifecycle(running ? 'stop' : 'start')}>{t(busy ? '处理中…' : running ? '停止网关' : '启动网关')}</button>
    </section>}
  </>
}
