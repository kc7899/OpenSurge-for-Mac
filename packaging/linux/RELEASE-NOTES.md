Ubuntu Server 24.04 x86_64 / amd64，后台服务 + Web 控制台，**IPv4 旁路由预览版**。

下载 `.deb` 并核对 `SHA256SUMS`。安装：`sudo apt install ./opensurge-gateway_0.3.4-ubuntu.2_amd64.deb`。安装只启动控制服务，不接管网络。

在电脑建立 SSH 转发：`ssh -N -L 16167:127.0.0.1:61767 USER@UBUNTU_IP`。在 Ubuntu 执行 `sudo opensurge-admin console`，30 秒内打开显示的一次性链接。导入自己的订阅、校验应用，然后在网络页启动。客户端 IPv4 网关和 DNS 都设置为 Ubuntu 的固定 LAN IPv4；保留主路由 DHCP。

复用 rc.4 的订阅管理、节点原名、稳定优先切换及经确认的 ChatGPT TLS 异常避让。健康线路不因其他节点更快而主动替换；已有候选之间切换使用 API，不重载整个网关。Linux 使用 TUN/auto-redirect、受管 nftables 表、接口级转发及 systemd-resolved 快照恢复。Web 控制服务以独立低权限用户运行，root Helper 只处理固定特权动作。

已通过 Go/Web 测试、真实隔离核心与 IPv4 下游数据面、严格 TLS 检测、真实私有 D-Bus DNS 恢复、安装登录/权限、导入应用、升级保留数据和卸载清理检查。准确源码提交和核心校验值见 `BUILD-INFO.txt`；安装、备份和回滚见 `INSTALL.md`。

限制：未签名预览包；尚无物理网卡、真实家庭客户端、真实 Tailnet/ACL 或 IPv6 验收。仅同 LAN IPv4，不接管 DHCP，不重写 Netplan，不自动恢复开机后的网关接管。IPv6 可能经主路由绕行，须在接入设备处理该默认路由。原设计中的自动登记/默认批准并未因移植而新增完成。请先验证一台测试设备，再迁移其他设备。

升级/卸载会停止网关，应安排维护窗口。私人数据保留在 `/var/lib/opensurge`，不包含在下载包中。本发布不会替换正在使用的 Mac 版，也不会自动安装到用户设备。
