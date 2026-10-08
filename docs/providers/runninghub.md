# Workflow RunningHub

RunningHub (runninghub.ai) chạy workflow ComfyUI trên đám mây. Gateway có một
**kho workflow**: mỗi mục biến một workflow RunningHub thành một model bình thường
của gateway (ví dụ `rh-h3-real-skin`), KSB gọi như mọi model khác qua API DC-Media.
Kho này không gắn với nơi chạy: hiện chỉ có nơi chạy `runninghub`; `comfyui`
(máy ComfyUI của mình / RunPod) đã chừa sẵn cho sau này.

Bản tiếng Anh, có bảng ánh xạ trường và danh sách API RunningHub: [en/runninghub.md](en/runninghub.md).

## KSB gọi thế nào

- Video (và âm thanh): `POST /v1/video/generations`, hỏi trạng thái như mọi tác vụ
  video; kết quả lấy qua gateway (`/v1/videos/{id}/content`, `result_url`).
- Ảnh: `POST /v1/images/generations` hoặc `/v1/images/edits`; gateway chờ RunningHub
  xong (tối đa 10 phút) rồi trả ảnh (`b64_json`, hoặc `url` nếu yêu cầu).
- Ảnh tham chiếu được đánh số theo thứ tự gửi: `image`, `images[]`, rồi
  `metadata.reference_images[]` — `@image1` của KSB là ảnh đầu tiên.
- Gửi nhiều ảnh/video/âm thanh hơn số ô workflow có → báo lỗi 400 ngay, không tải gì lên.

## Thêm một workflow

1. **Kênh** (làm một lần): Kênh → Thêm → loại **RunningHub**, dán API key RunningHub,
   giữ địa chỉ `https://www.runninghub.ai` (key trang Trung Quốc thì dùng
   `https://www.runninghub.cn`). Để trống danh sách model.
2. **Chuẩn bị trên RunningHub**: mở workflow trong tài khoản *của mình* (workflow cộng
   đồng thì sao chép/clone về trước), bật mọi đầu vào cần dùng (không để bypass),
   **chạy thử thành công một lần rồi lưu** — API từ chối workflow chưa từng chạy (lỗi 810).
3. Mô hình → **Workflow RunningHub** → **Thêm workflow**:
   - dán link `/workflow/<id>` rồi bấm **Lấy về** (tải JSON API bằng key của kênh),
     hoặc tải lên **JSON API** đã xuất — thêm JSON workflow đầy đủ nếu muốn thấy node
     đang tắt và gói node;
   - xem phần phân tích: **tệp model cần có** trong tài khoản RunningHub, gói node
     tuỳ chỉnh, các đầu vào có sẵn nhưng đang tắt;
   - kiểm tra đầu vào gợi ý (lời nhắc, ảnh, thời lượng có min/max, bảng tỉ lệ, seed)
     và node đầu ra;
   - mẫu lời nhắc (không bắt buộc): tiền tố/hậu tố, mẫu có `{{prompt}}`, đổi thẻ
     `@image1` → `<Picture {n}>`;
   - tên model, tiêu đề, loại đầu ra, cách tính tiền (*theo lượt* hoặc *× số giây*),
     GPU (*Plus* = 48 GB) → **Lưu**.
4. **Chạy thử** ngay trong khung đó (chạy thật một lần; RunningHub trừ xu tài khoản,
   gateway không tính tiền lần này).
5. Đặt giá model ở Cài đặt hệ thống → Thanh toán → **Định giá mô hình**. Nếu chọn
   *× số giây* thì giá là giá mỗi giây.
6. **Bật** workflow. Khi bật, tên model được thêm vào mọi kênh RunningHub.

## Model và LoRA

- Workflow chỉ dùng được tệp model mà tài khoản RunningHub sở hữu API key nạp được.
  Tệp trong thư viện model công khai của RunningHub thì tài khoản nào cũng dùng được.
- Clone workflow cộng đồng chạy được khi mọi model nằm trong thư viện công khai hoặc
  tác giả đã chia sẻ. Model riêng của tác giả sẽ lỗi khi chạy ("Value not in list" /
  thiếu tệp) — gateway báo *thiếu tệp model trong tài khoản RunningHub*. Khi đó phải
  tự tải tệp đó lên tài khoản mình (hoặc thay bằng model công khai) rồi chạy thử lại.
- API tải LoRA của RunningHub (`POST /api/openapi/getLoraUploadUrl` với `apiKey`,
  `loraName`, `md5Hex`, rồi PUT tệp lên link trả về) **chỉ dùng được với node
  RHLoraLoader**, không dùng với `LoraLoader` thường.

## Giới hạn

- Mỗi tệp tải lên tối đa 30 MB (giới hạn RunningHub).
- Số việc chạy cùng lúc tuỳ gói RunningHub; hàng đợi đầy (421) hoặc hết GPU (415)
  trả về HTTP 429 để gateway thử kênh khác.
- Thiếu số dư (416), sai key, lỗi workflow lúc gửi đều có giải thích; tác vụ lỗi được
  hoàn tiền như các kênh tác vụ khác.
- RunningHub không công bố link kết quả sống bao lâu; gateway chỉ đưa link của chính
  gateway và lấy từ RunningHub khi có người tải, nên KSB nên tải kết quả sớm.
