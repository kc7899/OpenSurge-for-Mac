# ChatGPT iPhone 证书提示：排查记录与能力边界

日期：2026-11-17。用户报告过去偶发“DigiCert Global Root G2”相关网络配置/证书警告，换代理节点后恢复。未取得故障当时的目标域名、证书链或 iPhone 校验日志，不能确诊历史根因。

## 已确认

- DigiCert Global Root G2 是证书机构公开列出的根证书。仅凭 CA 名称不能判定为伪造、过期或必须屏蔽。
- iOS 的证书信任涉及有效期、域名、签名和信任链；应用还可以增加自己的信任限制。Mac 普通 TLS 验证成功不能冒充 iPhone App 验证通过。
- 只读检查当时已应用的 OpenSurge profile，未发现 mitm、tls-interception、https-decrypt 或 sniffer 配置。未安装证书或修改任何验证策略。
- 通过当前 127.0.0.1:7890 出口，以系统 curl、不携带登录凭据测试：chatgpt.com HTTP 200、TLS verify 0；api.openai.com HTTP 421、TLS verify 0。后者只证明该请求的 TLS 验证通过，不能当作 API 或 App 功能可用。
- 换节点恢复提示应比较节点出口、解析路径及重连后的连接状态；不能仅据此认定 OpenSurge 伪造证书或设备遭到篡改。

## 当前版本

rc.3 修复动态 provider 节点显示和测量接口，不包含“ChatGPT 专项证书异常自动修复”。当前稳定选线的通用 HTTPS/地区检查可能发现不了特定 App/域名的证书失败。网关也不能直接读取 iPhone App 内部的信任校验结果。

## 专项恢复的设计条件（尚未实现）

- 严格验证目标主机的 TLS 主机名、有效期和信任链，重新建立连接复测，不使用账号或 API key。
- 只对重复确认的证书异常记录原因；HTTP 403、429、421 等不能直接当成证书失败。
- 通用对照目标可用，且同一服务通过符合地区策略的备用节点验证成功，才有证据把故障定位到当前路径。
- 如果所有候选同样失败，或只有 iPhone 报错而网关检测正常，显示无法确认原因；不反复轮换、重启、清空缓存或关闭 TLS 验证。
- 是否允许此类“单独服务异常”改变全网出口 IP，需要明确配置；其影响不同于仅切换某个 App 的出口。继续复用唯一检测/选择入口，不增加第二套定时器。
- 不屏蔽 DigiCert 根、不安装服务商根证书、不设置 InsecureSkipVerify，不承诺检测覆盖 ChatGPT iPhone App 的全部域名及额外信任策略。

参考：
- [DigiCert 根证书目录](https://knowledge.digicert.com/general-information/digicert-trusted-root-authority-certificates)
- [Apple TLS/ATS 信任检查](https://developer.apple.com/documentation/security/preventing-insecure-network-connections)
- [OpenAI API 连接错误说明](https://developers.openai.com/api/docs/guides/error-codes)；这是 API 通用说明，不能替代本次 iPhone 故障证据。
