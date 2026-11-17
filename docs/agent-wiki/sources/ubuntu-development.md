# Ubuntu Server 24.04 移植进度

目标：amd64、同网段 IPv4 整网旁路由、后台服务和共享 Web 控制台。分支 development/ubuntu-gateway，独立工作区 OpenSurge-ubuntu，基于 Mac rc.4。Mac 工作区和正式网络不修改。

当前为开发中，尚无可安装 Ubuntu 成品。已接入 Linux 网络快照/邻居/路由、nftables 独占表和 IP forwarding，以及按接口快照应用/恢复 systemd-resolved DNS 的平台适配，保持原生命周期和设置/设备数据库。

边界：不自动改 Netplan/地址，不自动停用 UFW 等现有防火墙；Linux 首版下游 IPv6 转发在受管链阻断，不能声称接管主路由直接发布的 IPv6。Host IPv6/下游 BPF 尚不支持。仅 root Helper 负责固定特权动作，Web 控制服务使用独立低权限用户；默认 Web 走 loopback/SSH 转发，后续 LAN 密码访问沿用原机制。

接下来必须完成：准确 Linux 核心构建、真实隔离 Linux gateway Start/Stop 和 DNS/代理/DIRECT/REJECT 数据面、失败回滚和非本项目规则保护，systemd/deb 安装升级卸载检查，共享 Web 平台说明，以及通过后的安装包交付。没有通过这些门槛不能标记完成。

参考：
- https://wiki.metacubex.one/en/config/inbound/tun/
- https://wiki.nftables.org/wiki-nftables/index.php/Atomic_rule_replacement
- https://manpages.ubuntu.com/manpages/noble/man1/resolvectl.1.html

注意：systemd-resolved 测试不能在只隔离 network namespace 的环境通过宿主 D-Bus 修改宿主 DNS；真实网络测试必须另隔离挂载 /run，或禁用宿主 DNS 协同并明确验证边界。
