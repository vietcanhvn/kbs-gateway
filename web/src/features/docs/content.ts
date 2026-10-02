/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// Trang Tài liệu dựng sẵn của KBS API (đường dẫn /docs).
//
// Nội dung nằm trong code để mọi bản cài đều có sẵn hướng dẫn đúng với giao
// diện của chính nó: địa chỉ API lấy từ nơi trang đang chạy, tên hệ thống lấy
// từ Cài đặt. Quản trị viên muốn dùng trang tài liệu riêng thì điền "Liên kết
// tài liệu" trong Cài đặt hệ thống - menu sẽ trỏ ra đó thay cho trang này.

export type GuideContext = {
  /** Tên hệ thống hiển thị, ví dụ "KBS API". */
  name: string
  /** Địa chỉ gốc của cổng, ví dụ "https://api.kimbox.studio". */
  origin: string
}

const FENCE = '```'

function vi({ name, origin }: GuideContext): string {
  return `# Hướng dẫn sử dụng ${name}

**${name}** là cổng API tổng hợp: một tài khoản, một khóa API, một địa chỉ duy nhất để gọi nhiều mô hình AI - viết chữ, tạo ảnh, tạo video, giọng nói và nhạc. Bạn nạp tiền trước, dùng tới đâu trừ tới đó; không có phí thuê bao.

## Bắt đầu trong 4 bước

1. **Tạo tài khoản** - bấm **Đăng ký** (hoặc **Đăng nhập**) ở góc trên bên phải.
2. **Nạp tiền** - vào **Bảng điều khiển → Ví**, ở khung **Nạp tiền** chọn số tiền, chọn **Chuyển khoản QR** (biểu tượng VietQR), rồi quét mã bằng ứng dụng ngân hàng. Số dư được cộng tự động sau khi chuyển khoản thành công.
3. **Tạo khóa API** - vào **Bảng điều khiển → Khóa API**, bấm **Tạo Khóa API**, đặt tên rồi sao chép khóa (bắt đầu bằng \`sk-\`).
4. **Dùng khóa** - dán *địa chỉ API* và *khóa* vào ứng dụng của bạn (xem các phần bên dưới).

## Việc gì làm ở mục nào

| Bạn muốn | Vào mục |
|---|---|
| Nạp tiền, xem số dư | **Ví** |
| Nhập mã đổi thưởng (mã tặng) | **Ví** → ô *Có mã không?* → **Đổi** |
| Xem lại các lần nạp | **Ví** → *Lịch sử đơn hàng* |
| Lấy liên kết giới thiệu, xem hoa hồng | **Ví** → *Chương trình Giới thiệu* |
| Tạo, tắt, xoá khóa API; đặt hạn ngạch cho từng khóa | **Khóa API** |
| Xem danh sách mô hình và giá | **Mô hình** (menu trên cùng) |
| Xem từng lần gọi đã tốn bao nhiêu | **Nhật ký sử dụng** |
| Theo dõi tác vụ tạo ảnh, tạo video | **Nhật ký tác vụ** |
| Thử mô hình ngay trên web, không cần viết code | **Sân chơi** |
| Xem biểu đồ mức dùng | **Tổng quan** |
| Đổi mật khẩu, thông tin cá nhân | **Hồ sơ** |

Các mục **Ví**, **Khóa API**, **Nhật ký…** nằm ở cột bên trái, sau khi bạn đăng nhập và bấm **Bảng điều khiển** trên menu.

## Nạp tiền

- Thanh toán bằng **chuyển khoản ngân hàng qua mã VietQR**. Tỷ giá và mức nạp tối thiểu hiện ngay trên trang **Ví**.
- Hãy chuyển **đúng số tiền và đúng nội dung** ghi trên mã QR để hệ thống tự nhận.
- Số dư tính bằng đô la Mỹ ($). Mỗi lần gọi mô hình trừ theo bảng giá ở mục **Mô hình**.
- Hết số dư thì lệnh gọi bị từ chối cho tới khi bạn nạp thêm; tài khoản và khóa API vẫn giữ nguyên.

## Giới thiệu bạn bè

- Liên kết giới thiệu của bạn nằm ở cuối trang **Ví**, mục *Chương trình Giới thiệu*. Gửi liên kết đó cho người khác đăng ký.
- Mỗi lần người bạn đã mời **nạp tiền**, bạn được cộng hoa hồng theo tỷ lệ ghi ngay trong mục đó (nếu chương trình đang bật).
- Hoa hồng vào ô *Đang chờ*. Bấm **Chuyển vào số dư** để dùng như tiền đã nạp. Bấm **Lịch sử hoa hồng** để xem từng khoản.

## Khóa API

- Mỗi khóa là một chuỗi bắt đầu bằng \`sk-\`. **Giữ kín như mật khẩu** - ai có khóa là tiêu được số dư của bạn.
- Nên tạo riêng một khóa cho mỗi ứng dụng, để khi cần chỉ phải tắt đúng khóa đó.
- Khi tạo có thể đặt hạn ngạch (số tiền tối đa khóa được tiêu) và ngày hết hạn cho khóa.
- Nếu lộ khóa: vào **Khóa API**, xoá khóa cũ rồi tạo khóa mới.

## Dùng với KimStudio Box (KSB)

Trong KSB mở **Cấu hình → KBS API** và điền:

- **Địa chỉ**: \`${origin}\`
- **Khóa**: khóa \`sk-…\` bạn vừa tạo

rồi bấm **Lưu & Đóng**. Muốn đổi mô hình thì chọn ở các dòng *Tạo ảnh*, *Sửa ảnh* trong cùng bảng đó.

## Dành cho lập trình viên

Địa chỉ gốc: \`${origin}\`. Mọi lệnh gọi cần tiêu đề \`Authorization: Bearer sk-…\`. Tên mô hình lấy ở mục **Mô hình** hoặc \`GET /v1/models\`.

| Việc | Địa chỉ |
|---|---|
| Danh sách mô hình | \`GET /v1/models\` |
| Trò chuyện, sinh chữ (chuẩn OpenAI) | \`POST /v1/chat/completions\` |
| Chuẩn Gemini | \`POST /v1beta/models/{model}:generateContent\` |
| Tạo ảnh, tạo video (tác vụ chạy nền) | \`POST /v1/video/generations\` |
| Hỏi trạng thái tác vụ | \`GET /v1/video/generations/{task_id}\` |
| Tải tệp kết quả | \`GET /v1/videos/{task_id}/content\` |

### Gọi mô hình chữ

${FENCE}bash
curl ${origin}/v1/chat/completions \\
  -H "Authorization: Bearer sk-KHOA_CUA_BAN" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "TEN_MO_HINH", "messages": [{"role": "user", "content": "Xin chào"}]}'
${FENCE}

Với thư viện OpenAI (Python), chỉ cần đổi địa chỉ và khóa:

${FENCE}python
from openai import OpenAI

client = OpenAI(base_url="${origin}/v1", api_key="sk-KHOA_CUA_BAN")
reply = client.chat.completions.create(
    model="TEN_MO_HINH",
    messages=[{"role": "user", "content": "Xin chào"}],
)
print(reply.choices[0].message.content)
${FENCE}

### Tạo ảnh hoặc video

Ảnh và video chạy nền theo ba bước: gửi tác vụ, hỏi trạng thái, tải kết quả.

${FENCE}bash
# 1. Gửi tác vụ - trả về task_id
curl ${origin}/v1/video/generations \\
  -H "Authorization: Bearer sk-KHOA_CUA_BAN" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "TEN_MO_HINH", "prompt": "A red bicycle under an oak tree at sunset", "width": 1328, "height": 752}'

# 2. Hỏi lại mỗi vài giây cho tới khi "status" là "succeeded" (hoặc "failed")
curl ${origin}/v1/video/generations/TASK_ID -H "Authorization: Bearer sk-KHOA_CUA_BAN"

# 3. Tải tệp kết quả
curl ${origin}/v1/videos/TASK_ID/content -H "Authorization: Bearer sk-KHOA_CUA_BAN" -o ket-qua.png
${FENCE}

Gửi kèm ảnh tham chiếu: một ảnh đặt ở trường \`image\` (địa chỉ ảnh hoặc data URL); từ hai ảnh trở lên đặt tất cả vào \`metadata.reference_images\`.

## Câu hỏi thường gặp

**Chuyển khoản rồi mà chưa thấy tiền?** Chờ 1-2 phút rồi tải lại trang **Ví**. Kiểm tra đã chuyển đúng số tiền và đúng nội dung trên mã QR. Vẫn chưa có thì gửi ảnh chụp giao dịch tới contact@kimbox.studio.

**Báo "số dư không đủ"?** Nạp thêm ở **Ví**. Mức đã tiêu xem ở **Nhật ký sử dụng**.

**Báo khóa không hợp lệ (lỗi 401)?** Kiểm tra đã dán đủ khóa, khóa chưa bị vô hiệu hóa, chưa hết hạn và chưa dùng hết hạn ngạch.

**Không thấy mô hình mình cần?** Chỉ gọi được các mô hình đang hiện ở mục **Mô hình**.

**Mô hình bị mờ, có dấu chấm than trong ứng dụng?** API của mô hình đó đang bảo trì, hoặc máy chủ của mô hình chỉ chạy vào những khung giờ nhất định (một số mô hình ảnh và video chạy trên máy chủ riêng của chúng tôi). Khi máy chủ hoạt động lại, mô hình tự dùng được, bạn không cần làm gì. Trong lúc chờ, hãy chọn mô hình khác.
`
}

function en({ name, origin }: GuideContext): string {
  return `# ${name} user guide

**${name}** is a unified API gateway: one account, one API key and one address for many AI models - text, image, video, speech and music. You top up first and pay only for what you use; there is no subscription.

## Get started in 4 steps

1. **Create an account** - click **Sign up** (or **Sign in**) at the top right.
2. **Top up** - go to **Console → Wallet**, pick an amount under **Add Funds**, choose the **QR bank transfer** method (VietQR logo) and scan the code with your banking app. The balance is credited automatically once the transfer succeeds.
3. **Create an API key** - go to **Console → API Keys**, click **Create API Key**, name it and copy the key (it starts with \`sk-\`).
4. **Use the key** - paste the *API address* and the *key* into your app (see below).

## Where to do what

| You want to | Go to |
|---|---|
| Top up, check your balance | **Wallet** |
| Enter a redemption code | **Wallet** → *Have a Code?* → **Redeem** |
| Review past top-ups | **Wallet** → *Order History* |
| Get your referral link, see your commission | **Wallet** → *Referral Program* |
| Create, disable or delete API keys; set a limit per key | **API Keys** |
| Browse models and prices | **Model Square** (top menu) |
| See what each call cost | **Usage Logs** |
| Follow image and video tasks | **Task Logs** |
| Try a model in the browser, no code | **Playground** |
| See usage charts | **Overview** |
| Change your password or profile | **Profile** |

**Wallet**, **API Keys** and the logs are in the left column after you sign in and open **Console**.

## Topping up

- Payment is a **bank transfer via a VietQR code**. The exchange rate and the minimum amount are shown on the **Wallet** page.
- Transfer **the exact amount with the exact note** printed on the QR code so it is matched automatically.
- The balance is in US dollars ($). Each call is charged at the price listed in **Model Square**.
- When the balance runs out, calls are refused until you top up; your account and keys are kept.

## Referring friends

- Your referral link is at the bottom of the **Wallet** page, under *Referral Program*. Share it so others sign up through it.
- Each time someone you invited **tops up**, you earn a commission at the rate shown in that section (when the programme is on).
- Commission lands in *Pending*. Press **Transfer to Balance** to spend it like topped-up money, and **Commission history** to see every payment.

## API keys

- A key is a string starting with \`sk-\`. **Keep it as secret as a password** - whoever holds it can spend your balance.
- Create one key per app, so you can disable just that key when needed.
- You can set a spending limit and an expiry date when creating a key.
- If a key leaks: open **API Keys**, delete it and create a new one.

## Using it with KimStudio Box (KSB)

In KSB open **Settings → KBS API** and fill in:

- **Address**: \`${origin}\`
- **Key**: the \`sk-…\` key you created

then click **Save & Close**. To change models, pick them in the *Generate image* and *Edit image* rows of the same dialog.

## For developers

Base address: \`${origin}\`. Every call needs the header \`Authorization: Bearer sk-…\`. Model names are listed in **Model Square** or by \`GET /v1/models\`.

| Task | Endpoint |
|---|---|
| List models | \`GET /v1/models\` |
| Chat, text generation (OpenAI format) | \`POST /v1/chat/completions\` |
| Gemini format | \`POST /v1beta/models/{model}:generateContent\` |
| Image and video generation (background task) | \`POST /v1/video/generations\` |
| Task status | \`GET /v1/video/generations/{task_id}\` |
| Download the result | \`GET /v1/videos/{task_id}/content\` |

### Calling a text model

${FENCE}bash
curl ${origin}/v1/chat/completions \\
  -H "Authorization: Bearer sk-YOUR_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "MODEL_NAME", "messages": [{"role": "user", "content": "Hello"}]}'
${FENCE}

With the OpenAI library (Python), only the address and the key change:

${FENCE}python
from openai import OpenAI

client = OpenAI(base_url="${origin}/v1", api_key="sk-YOUR_KEY")
reply = client.chat.completions.create(
    model="MODEL_NAME",
    messages=[{"role": "user", "content": "Hello"}],
)
print(reply.choices[0].message.content)
${FENCE}

### Generating an image or a video

Images and videos run in the background in three steps: submit, poll, download.

${FENCE}bash
# 1. Submit the task - returns a task_id
curl ${origin}/v1/video/generations \\
  -H "Authorization: Bearer sk-YOUR_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model": "MODEL_NAME", "prompt": "A red bicycle under an oak tree at sunset", "width": 1328, "height": 752}'

# 2. Poll every few seconds until "status" is "succeeded" (or "failed")
curl ${origin}/v1/video/generations/TASK_ID -H "Authorization: Bearer sk-YOUR_KEY"

# 3. Download the result file
curl ${origin}/v1/videos/TASK_ID/content -H "Authorization: Bearer sk-YOUR_KEY" -o result.png
${FENCE}

Reference images: one image goes in the \`image\` field (a URL or a data URL); two or more all go in \`metadata.reference_images\`.

## FAQ

**Transferred but no balance yet?** Wait 1-2 minutes and reload **Wallet**. Check that the amount and the note match the QR code. If it still has not arrived, send a screenshot of the transfer to contact@kimbox.studio.

**"Insufficient balance"?** Top up in **Wallet**. What you spent is in **Usage Logs**.

**Invalid key (error 401)?** Check that the whole key was pasted and that it is not disabled, expired or over its limit.

**A model is missing?** Only the models shown in **Model Square** can be called.

**A model is greyed out with an exclamation mark in the app?** Its API is under maintenance, or its server only runs at certain hours (some image and video models run on our own servers). When the server is back the model works again on its own. Meanwhile, pick another model.
`
}

/** Nội dung trang Tài liệu theo ngôn ngữ giao diện (tiếng Việt, còn lại tiếng Anh). */
export function buildGuideMarkdown(
  language: string | undefined,
  context: GuideContext
): string {
  return (language ?? '').toLowerCase().startsWith('vi')
    ? vi(context)
    : en(context)
}
