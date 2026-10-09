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
   `https://www.runninghub.cn`). Ô model bắt buộc: ghi tên model định dùng cho workflow
   (ví dụ `rh-h3-real-skin`); không trùng tên model kênh khác đang phục vụ.
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

## Một workflow, nhiều ô tham chiếu

Có thể mở sẵn nhiều ô ảnh / video / audio trong một workflow (mỗi ô gắn một tệp
mẫu, chạy thành công một lần rồi **Save**). Khi yêu cầu gửi ít tệp hơn số ô, gateway
tự rút dây các ô không dùng khỏi node chúng nối vào (gửi giá trị null qua
nodeInfoList), nên tệp mẫu không lọt vào kết quả. Ô bắt buộc (*required*) thì không
rút. Muốn giữ tệp mẫu của workflow cho ô trống, đặt `unused_media: "keep"` trong
input mapping.

## Workflow "video dài" (dòng thời gian, ComfyUI-Easy-Media)

Workflow dùng node **MultiTrack Editor** (`easy multiTrackEditor`) + **MultiTrack Project** (MiniMax H3):
cả bảng dòng thời gian là một ô chữ JSON `track_data`. Gán ô đó vai trò **`timeline`** trong input
mapping (ảnh / video / audio / lời nhắc không cần gán riêng). Mỗi lượt gọi, gateway dựng lại
`track_data` từ yêu cầu:

- `metadata.segments`: `[{prompt, seconds, mode, continuity, images, videos, audios}]`. `mode` theo
  Easy-Media: `ti2v` (số ảnh quyết định: 0 = T2V, 1 = I2V khung đầu, 2 = FL2V đầu + cuối, 3+ = FMLF2V),
  `t2v`, `i2v`, `fl2v`; `r2v` (ảnh tham chiếu; có video thành `rv2v`); `v2v` / `vi2v` (sửa video,
  có ảnh thành VI2V); `l2v` (ảnh cuối của đoạn là **khung cuối** của đoạn). `continuity` là `shot`
  (cảnh mới) hoặc `context` / `context_drift` (nối tiếp đoạn trước). `images` / `videos` / `audios`
  là số thứ tự tệp gửi kèm (0 = tệp đầu; bỏ trống `images` = mọi ảnh); mỗi đoạn tối đa 3 video,
  3 audio - tệp thứ k của đoạn nằm trên rãnh video / audio thứ k. Trong lời nhắc đoạn, `@image3`
  là ảnh thứ 3 gửi đi; gateway đánh số lại theo tệp của chính đoạn (`<Picture 1>`…).
- Không có `segments` thì gateway đọc dòng đánh dấu trong lời nhắc:
  `[Đoạn 2 – 8s – TI2V – context] …`; không có nữa thì cả lời nhắc là một đoạn.
- Audio chọn theo đoạn chỉ phát trong khoảng của đoạn đó. Khi không đoạn nào chọn audio, audio
  đầu tiên mặc định là **nhạc / tiếng của cả video** (khoá tiếng, nhân vật nhép theo);
  `metadata.audio_lock: false` thì chỉ là giọng tham chiếu. `audio_lock: true` luôn thêm audio
  đầu làm nhạc cả video.
- Tính tiền theo **tổng số giây các đoạn**. Mặc định đoạn 1 là `shot`, các đoạn sau `context`;
  có ảnh thì `r2v`, không thì `t2v`. Mỗi đoạn 1–20 giây, tối đa 24 đoạn.

## Độ phân giải (metadata.resolution)

Workflow tự đặt cỡ khung (ResolutionSelector, Easy-Media `resolution.megapixels`, một PrimitiveInt
cạnh dài nối vào node resize) thì gán ô đó vai trò:

- **`megapixels`**: số megapixel (ô `megapixels`, `resolution.megapixels`, hoặc PrimitiveFloat nối vào
  ô `megapixels` - bộ phân tích tự nhận). Gán được nhiều ô (workflow 2 lượt: lượt nháp + lượt chính).
- **`long_edge`**: cạnh dài tính bằng px (PrimitiveInt nối vào width/height của node resize) - gán tay.

Ô **"Giá trị ứng với 720p"** là giá trị gốc của workflow. Khách gửi `resolution` (360p, 480p, 720p,
1080p, 2k, 4k) thì gateway nhân giá trị gốc theo số điểm ảnh so với 720p (1080p = ×2,25 megapixel,
cạnh dài ×1,5); 720p hoặc không gửi = giữ nguyên workflow. Có min / max thì kẹp trong khoảng đó.
Tỷ lệ khung: gán vai trò `aspect_ratio` cho ô tỷ lệ (cả `resolution.aspect_ratio` của Easy-Media).

Tính tiền: giá theo giây là giá ở 720p; mức lớn hơn nhân theo số điểm ảnh (1080p ×2,25, 2k ×4,
4k ×9); mức nhỏ hơn giữ giá 720p.

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
