// Package webhook 提供事件接入前的安全与合法性校验能力：
//   - 来源签名（HMAC-SHA256）验签；
//   - 负载 JSON 合法性与大小校验；
//   - 事件类型是否在来源允许范围内的校验。
package webhook
