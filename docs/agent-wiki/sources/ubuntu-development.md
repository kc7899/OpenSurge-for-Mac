# Ubuntu Server 24.04 移植进度

目标：amd64、同网段 IPv4 整网旁路由、后台服务和共享 Web 控制台。分支 development/ubuntu-gateway，独立工作区 OpenSurge-ubuntu，基于 Mac rc.4。Mac 工作区和正式网络不修改。

已实现 Ubuntu IPv4 预览版代码、systemd 服务与 `.deb` 打包；完整自动门槛已在 [Ubuntu 验收运行](https://github.com/kc7899/OpenSurge-for-Mac/actions/runs/44110486710) 通过。最终产物随提交重新验收后发布。安装与恢复见 [Ubuntu 安装说明](../../ubuntu-install.md)。这些是已实现行为；原稳定选线与接入设计中的未实现项不因此变成已完成。

实现复用原网络快照、来源/store、配置应用事务、生命周期锁、恢复日志及稳定选线调度。Linux 适配 `ip -j`、一个带所有权标记的 nftables 表及指定下游接口的 forwarding；不写全局 `ip_forward`，避免其引发其他接口参数重置。防火墙先于转发/TUN加载，阻断 ICMP redirect 和核心消失时的裸转发；仅移除本项目带标记的表。systemd-resolved 的 DNS/domain 按接口身份快照、读回和恢复，不用 `revert` 重置无关设置。

root Helper 与 opensurge 低权限控制服务分离。配置/节点文件 root:opensurge 0640，控制 store 0700、token 0600；显式代理及核心 DNS 仅监听 loopback。Web 经 SSH 16167 转发至服务 61767，配置精确浏览器 Origin，保持 CSRF 检查。初始化不复制 Mac 运行状态或私人订阅，安装不启动网关；升级和卸载先执行原生命周期清理，保留私人数据。

独立检测绑定真实上游接口并使用接管前 DNS，避免失败主出口捕获备用检测。为兼容需要特权的 socket 接口绑定，沿用 root Helper 固定动作：只接受已保存候选 ID 与固定检测目标编号，要求当前启动会话、活跃进程身份及 profile digest 匹配；不接受任意节点、URL、可执行文件、接口或 CA。Web 服务不新增 capability。临时检测核心始终关闭 TUN/路由，仅监听 loopback；核心实际节点和 MATCH 规则均就绪后才检测。CI 包含低权限普通 TLS 测试、对应 root 绑定接口 TLS 测试、保存候选与配置漂移检查，以及活跃 TUN/REJECT 下公共地址候选仍能独立到达的证明。

边界：不自动改 Netplan/地址，不自动停用 UFW 等现有防火墙；Linux 首版下游 IPv6 转发在受管链阻断，不能声称接管主路由直接发布的 IPv6。Host IPv6/下游 BPF 尚不支持。仅 root Helper 负责固定特权动作，Web 控制服务使用独立低权限用户；默认 Web 走 loopback/SSH 转发，后续 LAN 密码访问沿用原机制。

已通过：全部 Go 单元测试、405 项共享 Web 测试与构建、锁定源码 Linux Mihomo 构建、真实核心多来源/API 切换与 provider 更新、严格 TLS 故障避让；独立 mount/net namespace 中下游 DNS/fake-IP、PROXY/DIRECT/REJECT、API 选择后 PID/配置不变、核心消失保护、DNS 冲突回滚、转发恢复及外部 nft 表保留；独立 D-Bus 中真实 systemd-resolved 接管/恢复与空域清理；实际 dpkg/systemd 安装、Web 登录/CSRF、低权限导入和 root Helper 应用、同版本重装保留配置/token及卸载保留数据。

找问题审查补齐了 Linux 启动先装 guard、LAN 接口级转发、SSH Origin、临时检测核心实际节点就绪后才检测（TCP/API version 就绪不等于节点已加载），以及安装权限/许可证遗漏。验证仍不等于物理家庭网络验收。未覆盖：物理网卡、iPhone、真实 Tailnet/ACL、IPv6，所有第三方防火墙组合、实机断电/睡眠恢复。源自动登记/默认批准功能未在 rc.4 基线完成；Linux 首版不宣称新增该功能。当前按既有 IPv4 规则允许未逐台登记的普通代理流量，实际登记/授权数据库沿用原实现。

流程只在完整门槛通过时上传产物；正式安装到用户 Mac 或 Ubuntu 以及迁移真实家庭网络不自动执行。主分支和 Mac 研发分支保持基线，本分支 PR 仅指向自己的 fork。

发布：专用 Ubuntu CI 的 linux job 全部成功后，只有自己的 Ubuntu 分支、提交标题显式含 `[publish-ubuntu]` 时才创建预发布。其余提交只测试和上传 Actions 产物。发布 job 单独申请 contents:write；不改默认分支、不覆盖已有不同提交的 tag/安装包。后续预览统一修改 packaging/linux/VERSION，构建、测试包名、Web release tag 和发布 tag 从该版本推导；同步维护安装文档中的示例命令。公开文件名使用连字符替代 Debian 版本中的 ~，避免 GitHub 对文件名改写造成校验清单不匹配。安装包内保留许可证及准确源码提交。

参考：
- https://wiki.metacubex.one/en/config/inbound/tun/
- https://wiki.nftables.org/wiki-nftables/index.php/Atomic_rule_replacement
- https://manpages.ubuntu.com/manpages/noble/man1/resolvectl.1.html
- https://docs.kernel.org/networking/ip-sysctl.html

注意：systemd-resolved 测试不能在只隔离 network namespace 的环境通过宿主 D-Bus 修改宿主 DNS；真实网络测试必须另隔离挂载 /run，或禁用宿主 DNS 协同并明确验证边界。
