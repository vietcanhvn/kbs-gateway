# Mang cấu hình KBS từ gateway này sang gateway khác

Giá (+10%), công thức tính tiền theo token, cài đặt payOS và các kênh (kể cả
tuyến workflow ComfyUI) nằm trong CƠ SỞ DỮ LIỆU của gateway, không nằm trong
code. Cài code mới lên máy khác thì các thứ đó KHÔNG tự đi theo - dùng 2 bước dưới.

## 1. Xuất (máy nguồn)

```bash
docker cp dramaclaw-newapi-1:/data/one-api.db /tmp/gw-copy.db
python3 tools/kbs-config/export.py /tmp/gw-copy.db kbs-config.json
```

`kbs-config.json` KHÔNG có key kênh, key payOS, địa chỉ máy chủ. Nhưng có địa
chỉ máy ComfyUI (RunPod) - gửi riêng, đừng đưa lên git (kho này công khai).

## 2. Nhập (máy đích)

1. Đăng nhập gateway đích bằng tài khoản root -> Hồ sơ -> tạo **mã truy cập**.
2. Xem trước sẽ đổi gì:

```bash
GATEWAY_URL=https://api.kimbox.studio GATEWAY_ADMIN_TOKEN=<mã-truy-cập> node tools/kbs-config/import.mjs kbs-config.json
```

3. Ghi thật: thêm `--apply`. Kênh chưa có ở máy đích chỉ được tạo khi có key:
   tạo tệp `channel-keys.json` (để trên máy, không đưa lên git), ví dụ
   `{"Gemini-API": "AIza...", "BytePlus Seedance": "...", "Minimax H3": "..."}`,
   rồi thêm `--keys channel-keys.json`. Kênh đã có thì chỉ cập nhật, giữ key cũ.

## 3. Tự làm trong giao diện gateway đích

- Cổng thanh toán: xác nhận điều khoản tuân thủ; Địa chỉ máy chủ.
- payOS (VietQR): dán API Key + Checksum Key, bật công tắc.
- Kiểm tra: Định giá mô hình phải thấy các dòng "base · $0.55 / $3.3/M"...

Chạy lại nhiều lần không sao (ghi đè cùng giá trị). Kiểm thử: `node --test tools/kbs-config/import.test.mjs`.
