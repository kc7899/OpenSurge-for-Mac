# ChatGPT iPhone 证书提示：排查记录与能力边界

日期：2026-11-17。用户报告过去偶发“DigiCert Global Root G2”相关网络配置/证书警告，换代理节点后恢复。未取得故障当时的目标域名、证书链或 iPhone 校验日志，不能确诊历史根因。

## 已确认

- DigiCert Global Root G2 是证书机构公开列出的根证书。仅凭 CA 名称不能判定为伪造、过期或必须屏蔽。
- iOS 的证书信任涉及有效期、域名、签名和信任链；应用还可以增加自己的信任限制。Mac 普通 TLS 验证成功不能冒充 iPhone App 验证通过。
- 只读检查当时已应用的 OpenSurge profile，未发现 mitm、tls-interception、https-decrypt 或 sniffer 配置。未安装证书或修改任何验证策略。
- 通过当前 127.0.0.1:7890 出口，以系统 curl、不携带登录凭据测试：chatgpt.com HTTP 200、TLS verify 0；api.openai.com HTTP 421、TLS verify 0。后者只证明该请求的 TLS 验证通过，不能当作 API 或 App 功能可用。
- 换节点恢复提示应比较节点出口、解析路径及重连后的连接状态；不能仅据此认定 OpenSurge 伪造证书或设备遭到篡改。

## rc.3 的范围

rc.3 修复动态 provider 节点显示和测量接口，不包含“ChatGPT 专项证书异常自动修复”。当前稳定选线的通用 HTTPS/地区检查可能发现不了特定 App/域名的证书失败。网关也不能直接读取 iPhone App 内部的信任校验结果。

## rc.4：已实现并经隔离验证的专项恢复

用户已明确允许：确认当前节点的 ChatGPT 证书异常，且合格备用验证通过时，可以改变全网出口 IP。

- `chatgpt_tls` 默认开启，受自动切换总开关控制；明确关闭会保留，旧设置读取不改写文件，旧页面保存其他设置不会意外关闭它。安装/正式启动仍由用户在维护窗口执行。
- 检查固定公开域名 `chatgpt.com`、`api.openai.com`，使用标准 TLS 主机名、有效期和信任链验证，只做握手，不发送 HTTP、账号或 API key。它不覆盖 iPhone App 的全部域名或额外信任策略，也不完全复刻客户端 DNS/分流。
- 同一目标两次得到同类证书错误后才确认；候选沿用当前来源优先、订阅顺序、地区与低延迟排序，只有通用连通/地区合格且服务证书通过的候选才可用于这次恢复。
- 找到合格备用后再次核对当前路径；当前已恢复或结果不确定则保留当前线路。所有备用失败或无法确认时也保留通用仍健康的线路，显示告警，不为单独服务异常关闭全网代理。
- 失败候选沿用配置的冷却时长，冷却后需要两次成功恢复；当前失效不受候选冷却限制。健康备用投入后保持选择，不因旧节点恢复抢回。
- 全部工作复用现有 Monitor 与 Executor，没有第二个定时器；结果绑定既有设置/来源/指纹/进程版本，切换前核对当前状态。较慢扫描结束后重新验证过期的当前证据。
- 通过受管 selector API 选择，不重载主配置、重启核心或修改 DNS/系统代理；正式网络在研发中没有被替换。
- 共享 Web/原生控制台增加开关、当前证书验证状态、目标、错误类别、检查时间及人类可读的切换原因。

验收：受控有效/错误域名/过期/不受信任证书；单次异常、超时、全部候选失败、禁止地区、关闭开关、恢复复核、版本竞态、冷却两次恢复和慢扫描；真实临时 Mihomo 完成错误证书复测→备用验证→一次 API 切换→有效 HTTPS（即使 HTTP 403）→健康保持；主配置未重写。测试 CA 仅在隔离测试内存中使用，没有安装到系统。
包构建脚本现在为完整 Go 测试设置 `OPENSURGE_TEST_CORE`，使用即将打包的准确核心，防止真实核心门槛因未设置变量被跳过。

**以上不等于已复现或修复历史 iPhone 警告；真机及长期运行仍待验证。**

## 专项恢复约束

- 严格验证目标主机的 TLS 主机名、有效期和信任链，重新建立连接复测，不使用账号或 API key。
- 只对重复确认的证书异常记录原因；HTTP 403、429、421 等不能直接当成证书失败。
- 通用对照目标可用，且同一服务通过符合地区策略的备用节点验证成功，才有证据把故障定位到当前路径。
- 如果所有候选同样失败，或只有 iPhone 报错而网关检测正常，显示无法确认原因；不反复轮换、重启、清空缓存或关闭 TLS 验证。
- 用户已经授权此类确认后的单独服务异常可改变全网出口 IP；控制台可关闭。只复用唯一检测/选择入口，不增加第二套定时器。
- 不屏蔽 DigiCert 根、不安装服务商根证书、不设置 InsecureSkipVerify，不承诺检测覆盖 ChatGPT iPhone App 的全部域名及额外信任策略。

参考：
- [DigiCert 根证书目录](https://knowledge.digicert.com/general-information/digicert-trusted-root-authority-certificates)
- [Apple TLS/ATS 信任检查](https://developer.apple.com/documentation/security/preventing-insecure-network-connections)
- [OpenAI API 连接错误说明](https://developers.openai.com/api/docs/guides/error-codes)；这是 API 通用说明，不能替代本次 iPhone 故障证据。
