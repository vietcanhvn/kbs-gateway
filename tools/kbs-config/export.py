#!/usr/bin/env python3
"""Xuất cấu hình KBS (giá, công thức tính tiền, payOS, kênh) từ một gateway.

Đọc một BẢN SAO cơ sở dữ liệu SQLite của gateway (không đụng bản đang chạy):

    docker cp dramaclaw-newapi-1:/data/one-api.db /tmp/gw-copy.db
    python3 tools/kbs-config/export.py /tmp/gw-copy.db kbs-config.json

KHÔNG xuất: key của kênh, key / mật khẩu payOS, địa chỉ máy chủ, địa chỉ
callback, xác nhận tuân thủ thanh toán (người quản trị nơi nhập tự làm).
Tệp kết quả vẫn chứa địa chỉ máy ComfyUI - đừng đưa lên kho công khai.
"""
import json
import sqlite3
import sys
from datetime import datetime, timezone

# Cài đặt mang sang. Mọi thứ khác (key, địa chỉ, tuân thủ...) giữ theo nơi nhập.
OPTION_KEYS = [
    "ModelPrice", "ModelRatio", "CompletionRatio", "CacheRatio", "CreateCacheRatio",
    "ImageRatio", "AudioRatio", "AudioCompletionRatio",
    "billing_setting.billing_mode", "billing_setting.billing_expr",
    "GroupRatio", "TopupGroupRatio", "UserUsableGroups",
    "payment_setting.amount_options", "payment_setting.amount_discount",
    "PayOSClientId", "PayOSUnitPrice", "PayOSMinTopUp",
    "SelfUseModeEnabled", "SystemName", "Footer", "general_setting.docs_link", "theme.frontend",
    # Điều khoản sử dụng và Chính sách quyền riêng tư (đã được chủ dịch vụ duyệt).
    "legal.user_agreement", "legal.privacy_policy",
]

# Trường của kênh mang sang; `key` không bao giờ xuất.
CHANNEL_FIELDS = [
    "name", "type", "status", "models", "base_url", "model_mapping", "group", "priority",
    "weight", "tag", "test_model", "auto_ban", "status_code_mapping", "setting", "settings",
    "param_override", "header_override", "other", "remark",
]


def main(db_path, out_path):
    db = sqlite3.connect(db_path)
    db.row_factory = sqlite3.Row
    options = {
        row["key"]: row["value"]
        for row in db.execute("select key, value from options")
        if row["key"] in OPTION_KEYS
    }
    columns = {row[1] for row in db.execute("pragma table_info(channels)")}
    fields = [f for f in CHANNEL_FIELDS if f in columns]
    channels = [
        {f: row[f] for f in fields}
        for row in db.execute("select " + ", ".join(f'"{f}"' for f in fields) + " from channels order by id")
    ]
    out = {
        "format": "kbs-config/1",
        "exported_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "options": options,
        "channels": channels,
    }
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False, indent=2)
    print(f"Đã xuất {len(options)} cài đặt, {len(channels)} kênh -> {out_path}")
    for ch in channels:
        print(f"  - kênh '{ch['name']}' (loại {ch['type']}), model: {ch.get('models')}")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("Cách dùng: export.py <bản-sao-one-api.db> <kbs-config.json>")
    main(sys.argv[1], sys.argv[2])
