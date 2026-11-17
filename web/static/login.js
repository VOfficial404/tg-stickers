// 发出请求不等于短信已送达；仅在服务端确认发送渠道可用时显示发送提示。
export function codeDelivery(state) {
  return state.code_sent
    ? `Telegram 返回的发送渠道：${state.code_via || '未说明'}。`
    : `当前验证方式：${state.code_via || 'Telegram 未确认已发送验证码'}。`;
}

export function resendStatus(state, now = Date.now()) {
  const remaining = Math.max(0, Math.ceil((Number(state.code_retry_at || 0) * 1000 - now) / 1000));
  if (!state.code_can_resend) return { disabled: true, label: 'Telegram 未提供重发渠道' };
  if (remaining > 0) return { disabled: true, label: `${remaining} 秒后重发` };
  return { disabled: false, label: state.code_next ? `改用${state.code_next}重发` : '重新发送验证码' };
}
