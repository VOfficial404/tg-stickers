import { expect, test } from 'bun:test';
import { codeDelivery, resendStatus } from './static/login.js';

test('unsupported delivery does not claim a code was sent', () => {
  expect(codeDelivery({code_sent:false,code_via:'官方 App 的 Firebase 验证'})).not.toContain('已发');
  expect(codeDelivery({code_sent:true,code_via:'短信'})).toContain('短信');
});

test('resend waits for Telegram timeout and cannot invent a delivery fallback', () => {
  const state={code_can_resend:true,code_next:'短信',code_retry_at:1060};
  expect(resendStatus(state, 1000000)).toEqual({disabled:true,label:'60 秒后重发'});
  expect(resendStatus(state, 1060000)).toEqual({disabled:false,label:'改用短信重发'});
  expect(resendStatus({...state,code_can_resend:false}, 2000000).disabled).toBe(true);
});
