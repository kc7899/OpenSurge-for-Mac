# OpenSurge 独立研发说明

本研发分支基于 [YTwsy/OpenSurge-for-Mac](https://github.com/YTwsy/OpenSurge-for-Mac)，
保留上游历史、署名、GPL-3.0-only 许可证及第三方声明。

## 基线与状态

- 上游：`https://github.com/YTwsy/OpenSurge-for-Mac.git`
- 基线：`a05aa1f2c3cae45578991f2f912d049a0f36e17e`，上游 master 的 v0.3.3 发布合并提交。
- 研发分支：`development/stable-gateway`。
- 已提供 0.3.4-rc.4 arm64 本地研发预览安装包，已接通稳定选线后台、共享界面、订阅节点池热更新；rc.3 补齐动态池原名与实际检测接口，rc.4 接入经确认的 ChatGPT 证书异常自动避障；整套功能尚未完成，未创建正式 GitHub Release。实际范围见 [研发进度](docs/agent-wiki/sources/stable-development-progress.md)。
- 旧实验实现不整体迁入；兼容修复、UI 和测试经逐项审查后分别迁移。

## 本轮范围

1. 多订阅稳定优先故障切换，支持任意名称和订阅数量。
2. 日常线路检测与已有节点切换不重载网关、不重启服务。
3. 局域网设备自动发现登记、默认放行和独立的 Tailscale 自动授权设置。
4. 共享 Web/桌面控制台展示设置、实际生效状态和切换原因。

详细方案、分阶段工作和验收矩阵见
[稳定选线与设备接入设计](docs/agent-wiki/sources/decisions/stable-routing-and-admission.md)。

## 开发边界

- 开发使用独立配置、store、端口和隔离测试核心，不连接正式实例执行写操作。
- 不将个人订阅 URL、节点密钥、控制台 token、Tailscale 密钥、设备清单或运行日志提交到仓库。
- 保留原项目配置草稿、应用事务、特权 Helper 和恢复边界；新增行为必须有明确的所有权。
- 运行中的安装不因源码更新或文档提交而替换。发布与安装是独立步骤。
- 验证结论注明范围；单元测试不代表真实局域网、IPv6 或 Tailscale 已验收。
