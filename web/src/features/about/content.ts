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
// Trang Giới thiệu mặc định của KBS API.
//
// Hiện khi quản trị viên chưa điền mục "Giới thiệu" trong Cài đặt hệ thống.
// Điền nội dung ở đó (Markdown, HTML hoặc một địa chỉ web) thì nội dung đó thay
// cho trang này.
//
// Dòng cuối (New API / One API / AGPL và đường dẫn mã nguồn) là ghi công bắt
// buộc theo giấy phép AGPL v3.0 của mã nguồn gốc - đừng bỏ.

const SOURCE_URL = 'https://github.com/vietcanhvn/kbs-gateway'
const UPSTREAM_URL = 'https://github.com/QuantumNous/new-api'
const ONE_API_URL = 'https://github.com/songquanpeng/one-api'

function vi(name: string): string {
  return `# ${name} - cổng API tổng hợp cho AI sáng tạo

**${name}** gom nhiều mô hình AI - viết chữ, tạo ảnh, tạo video, giọng nói và nhạc - về một cổng duy nhất. Một tài khoản, một khóa API, một số dư: bạn không phải đăng ký và nạp tiền riêng ở từng nhà cung cấp.

## Vì sao dùng ${name}

- **Một khóa cho mọi mô hình.** Đổi mô hình chỉ là đổi tên mô hình, không phải đổi cách kết nối.
- **Trả theo lượng dùng.** Nạp trước, dùng tới đâu trừ tới đó. Không thuê bao. Giá từng mô hình công khai ở [Quảng trường mô hình](/pricing).
- **Nạp tiền bằng VietQR.** Chuyển khoản từ ngân hàng Việt Nam, số dư cộng tự động.
- **Tương thích chuẩn OpenAI và Gemini.** Dùng được ngay với các công cụ và thư viện sẵn có.
- **Minh bạch.** Mỗi lần gọi đều được ghi lại: mô hình nào, lúc nào, hết bao nhiêu.
- **Sinh ra cho người làm phim AI.** ${name} là cổng AI của KimStudio Box (KSB) - bộ công cụ làm phim AI từ kịch bản, nhân vật, bối cảnh, phân cảnh tới video.

## Dành cho ai

- **Người dùng KimStudio Box**: nạp tiền một nơi, dùng mọi tính năng AI trong KSB.
- **Lập trình viên và nhóm sản phẩm**: gọi nhiều mô hình qua một địa chỉ, một hoá đơn.
- **Nhóm sáng tạo nội dung**: thử nhanh mô hình ảnh, video mới mà không phải tự cài đặt.

## Bắt đầu

1. Đăng ký tài khoản.
2. Vào **Ví** để nạp tiền.
3. Vào **Khóa API** để tạo khóa.
4. Xem cách dùng ở trang [Tài liệu](/docs).

---

${name} được xây dựng trên mã nguồn mở [New API](${UPSTREAM_URL}) (dựa trên [One API](${ONE_API_URL})), phát hành theo giấy phép AGPL v3.0. Mã nguồn của bản này: [${SOURCE_URL.replace('https://', '')}](${SOURCE_URL}).
`
}

function en(name: string): string {
  return `# ${name} - one API gateway for creative AI

**${name}** brings many AI models - text, image, video, speech and music - behind a single gateway. One account, one API key, one balance: no separate sign-up and top-up at every provider.

## Why ${name}

- **One key for every model.** Switching models means changing the model name, not the integration.
- **Pay as you go.** Top up first, pay only for what you use. No subscription. Prices are public in [Model Square](/pricing).
- **Top up with VietQR.** A transfer from a Vietnamese bank, credited automatically.
- **OpenAI and Gemini compatible.** Works with the tools and libraries you already use.
- **Transparent.** Every call is logged: which model, when, and what it cost.
- **Built for AI filmmakers.** ${name} is the AI gateway of KimStudio Box (KSB), the AI filmmaking toolkit - from script, characters, settings and scenes to video.

## Who it is for

- **KimStudio Box users**: one place to top up, every AI feature in KSB.
- **Developers and product teams**: many models through one address and one bill.
- **Content teams**: try new image and video models without hosting them.

## Get started

1. Create an account.
2. Open **Wallet** to top up.
3. Open **API Keys** to create a key.
4. Read the [Docs](/docs).

---

${name} is built on the open-source [New API](${UPSTREAM_URL}) (based on [One API](${ONE_API_URL})), released under the AGPL v3.0 license. Source code of this build: [${SOURCE_URL.replace('https://', '')}](${SOURCE_URL}).
`
}

/** Nội dung Giới thiệu mặc định theo ngôn ngữ giao diện. */
export function buildAboutMarkdown(
  language: string | undefined,
  name: string
): string {
  return (language ?? '').toLowerCase().startsWith('vi') ? vi(name) : en(name)
}
