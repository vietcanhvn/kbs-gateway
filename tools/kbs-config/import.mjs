#!/usr/bin/env node
// Nhập cấu hình KBS (tệp do export.py tạo) vào một gateway qua API quản trị.
//
//   GATEWAY_URL=https://api.kimbox.studio \
//   GATEWAY_ADMIN_TOKEN=<mã truy cập cá nhân của tài khoản root> \
//   node tools/kbs-config/import.mjs kbs-config.json            # chỉ xem sẽ đổi gì
//   node tools/kbs-config/import.mjs kbs-config.json --apply    # ghi thật
//   node tools/kbs-config/import.mjs kbs-config.json --apply --keys channel-keys.json
//
// - Cài đặt (giá, công thức tính tiền, payOS...) ghi qua PUT /api/option/.
// - Kênh tìm theo TÊN: đã có thì cập nhật (giữ nguyên key đang có), chưa có thì
//   chỉ tạo khi --keys có key cho kênh đó: {"Gemini-API": "AIza...", ...}.
//   Tệp key để trên máy, KHÔNG đưa lên git.
// - Không đụng: key / mật khẩu payOS, địa chỉ máy chủ, callback, xác nhận tuân
//   thủ, bật payOS - người quản trị nơi nhập tự làm trong giao diện.
//
// Chạy lại nhiều lần cũng được: lần sau ghi đè đúng giá trị đó.

import { readFile } from 'node:fs/promises';

const CHANNEL_UPDATE_FIELDS = [
  'name', 'type', 'models', 'base_url', 'model_mapping', 'group', 'priority', 'weight', 'tag',
  'test_model', 'auto_ban', 'status_code_mapping', 'setting', 'settings', 'param_override',
  'header_override', 'other', 'remark',
];

export const parseArgs = (argv) => {
  const args = { file: '', apply: false, keys: '' };
  for (let i = 0; i < argv.length; i += 1) {
    const a = argv[i];
    if (a === '--apply') args.apply = true;
    else if (a === '--keys') args.keys = argv[++i] || '';
    else if (!args.file) args.file = a;
  }
  return args;
};

const pick = (obj, fields) =>
  Object.fromEntries(fields.filter((f) => obj[f] !== undefined && obj[f] !== null).map((f) => [f, obj[f]]));

export const createClient = ({ baseUrl, token, fetchImpl = fetch }) => {
  const root = String(baseUrl || '').replace(/\/+$/, '');
  const call = async (method, path, body) => {
    const res = await fetchImpl(`${root}${path}`, {
      method,
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
    let json = null;
    try {
      json = await res.json();
    } catch {
      /* trả lời không phải JSON */
    }
    if (!res.ok || !json || json.success === false) {
      throw new Error(`${method} ${path} -> ${res.status} ${json?.message || ''}`.trim());
    }
    return json.data;
  };
  return {
    setOption: (key, value) => call('PUT', '/api/option/', { key, value: String(value) }),
    listChannels: async () => {
      const data = await call('GET', '/api/channel/?p=1&page_size=500');
      return Array.isArray(data) ? data : data?.items || [];
    },
    updateChannel: (channel) => call('PUT', '/api/channel/', channel),
    addChannel: (channel) => call('POST', '/api/channel/', { mode: 'single', channel }),
  };
};

/** Lên kế hoạch + (nếu apply) thực hiện. Trả về danh sách dòng báo cáo. */
export const runImport = async ({ config, client, apply, channelKeys = {} }) => {
  if (config?.format !== 'kbs-config/1') throw new Error('Tệp không phải kbs-config/1');
  const report = [];

  for (const [key, value] of Object.entries(config.options || {})) {
    report.push(`cài đặt ${key}`);
    if (apply) await client.setOption(key, value);
  }

  const existing = await client.listChannels();
  for (const channel of config.channels || []) {
    const fields = pick(channel, CHANNEL_UPDATE_FIELDS);
    const match = existing.find((c) => c.name === channel.name);
    if (match) {
      report.push(`kênh '${channel.name}': cập nhật (giữ key đang có)`);
      if (apply) await client.updateChannel({ ...fields, id: match.id });
    } else if (channelKeys[channel.name]) {
      report.push(`kênh '${channel.name}': tạo mới`);
      if (apply) await client.addChannel({ ...fields, key: channelKeys[channel.name] });
    } else {
      report.push(`kênh '${channel.name}': BỎ QUA - chưa có trên gateway này và --keys không có key cho nó`);
    }
  }
  return report;
};

const main = async () => {
  const args = parseArgs(process.argv.slice(2));
  const baseUrl = process.env.GATEWAY_URL;
  const token = process.env.GATEWAY_ADMIN_TOKEN;
  if (!args.file || !baseUrl || !token) {
    console.error('Cách dùng: GATEWAY_URL=... GATEWAY_ADMIN_TOKEN=... node import.mjs kbs-config.json [--apply] [--keys channel-keys.json]');
    process.exit(1);
  }
  const config = JSON.parse(await readFile(args.file, 'utf8'));
  const channelKeys = args.keys ? JSON.parse(await readFile(args.keys, 'utf8')) : {};
  const client = createClient({ baseUrl, token });
  const report = await runImport({ config, client, apply: args.apply, channelKeys });
  console.log(args.apply ? 'ĐÃ ÁP DỤNG:' : 'SẼ ÁP DỤNG (chạy lại với --apply để ghi):');
  for (const line of report) console.log(`  - ${line}`);
  console.log(
    '\nViệc tự làm trong giao diện gateway: dán key payOS (API Key, Checksum Key) rồi bật payOS; ' +
      'xác nhận điều khoản tuân thủ ở Cổng thanh toán; Địa chỉ máy chủ; key của kênh mới tạo.'
  );
};

if (import.meta.url === `file://${process.argv[1]}`) {
  main().catch((error) => {
    console.error(`Lỗi: ${error.message}`);
    process.exit(1);
  });
}
