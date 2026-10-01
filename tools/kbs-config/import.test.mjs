// node --test tools/kbs-config/import.test.mjs
import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createClient, parseArgs, runImport } from './import.mjs';

const config = {
  format: 'kbs-config/1',
  options: { ModelPrice: '{"lyria-3.5":0.088}', 'billing_setting.billing_mode': '{}' },
  channels: [
    { name: 'Gemini-API', type: 24, status: 1, models: 'gemini-3-flash-preview', settings: '{}' },
    { name: 'BytePlus Seedance', type: 54, status: 1, models: 'byteplus-seedance-2.0' },
    { name: 'FAL AI', type: 61, status: 2, models: 'seedance-2.0-fast' },
  ],
};

const fakeGateway = () => {
  const calls = [];
  const fetchImpl = async (url, init) => {
    calls.push({ method: init.method, path: url.replace('http://gw', ''), body: init.body ? JSON.parse(init.body) : undefined, auth: init.headers.Authorization });
    const data = init.method === 'GET' ? { items: [{ id: 7, name: 'Gemini-API' }] } : null;
    return { ok: true, status: 200, json: async () => ({ success: true, data }) };
  };
  return { calls, client: createClient({ baseUrl: 'http://gw/', token: 'PAT', fetchImpl }) };
};

test('mặc định chỉ xem, không ghi gì', async () => {
  const { calls, client } = fakeGateway();
  await runImport({ config, client, apply: false });
  assert.equal(calls.filter((c) => c.method !== 'GET').length, 0);
});

test('--apply: ghi cài đặt, cập nhật kênh có sẵn mà giữ key, tạo kênh mới khi có key', async () => {
  const { calls, client } = fakeGateway();
  const report = await runImport({ config, client, apply: true, channelKeys: { 'BytePlus Seedance': 'k-test' } });

  assert.equal(calls.filter((c) => c.path === '/api/option/').length, 2);
  const update = calls.find((c) => c.method === 'PUT' && c.path === '/api/channel/');
  assert.equal(update.body.id, 7);
  assert.equal('key' in update.body, false, 'không gửi key -> gateway giữ key đang có');
  assert.equal('status' in update.body, false, 'gateway từ chối trường status khi cập nhật');

  const add = calls.find((c) => c.method === 'POST');
  assert.equal(add.body.mode, 'single');
  assert.equal(add.body.channel.key, 'k-test');
  assert.equal(add.auth, 'Bearer PAT');

  assert.ok(report.some((line) => line.includes("'FAL AI': BỎ QUA")), 'không có key thì không tạo kênh');
});

test('từ chối tệp không đúng định dạng', async () => {
  const { client } = fakeGateway();
  await assert.rejects(runImport({ config: { format: 'x' }, client, apply: false }), /kbs-config\/1/);
});

test('đọc tham số dòng lệnh', () => {
  assert.deepEqual(parseArgs(['a.json', '--apply', '--keys', 'k.json']), { file: 'a.json', apply: true, keys: 'k.json' });
});
