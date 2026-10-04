# CLAUDE.md — Project Conventions for new-api

@AGENTS.md

## Claude Code

- Follow the shared project instructions imported from `AGENTS.md`.

## ⛔ Nhánh làm việc duy nhất: `studio`

**Không dùng nhánh `main`.** `main` là bản cũ, thiếu nhiều commit, không triển khai và không sửa. Ngày 04/10/2026 đã có hai lần dev lấy nhầm `main` rồi phải làm lại.

Việc lớn thì tách nhánh riêng từ `studio` rồi gộp lại vào `studio`.

## Làm việc nhiều máy (máy Win và máy Mac)

Hai máy cùng sửa repo này. **Không bao giờ sửa cùng lúc.** Trình tự bắt buộc:

**Đầu mỗi phiên hoặc mỗi việc:**
1. `git pull` nhánh đang dùng.
2. Đọc **5 dòng cuối** của `NHAT_KY_MA.md` ở gốc repo để biết máy kia vừa sửa gì.
3. Xem hộp thư của máy mình có việc nào `Trạng thái: MOI` không thì làm trước, hoặc báo lại:
   - Máy Mac: `G:\My Drive\KMG.AI.Trụ sở trợ lý\10.Hộp thư\KT-MAC-KBS\viec`
   - Máy Win: `D:\Auto_Claude\giao-viec\viec`

**Xong mỗi thay đổi:**
1. Commit và **push NGAY**. Không để mã chưa đẩy qua đêm.
2. Thêm một dòng vào `NHAT_KY_MA.md` theo mẫu:
   `giờ | máy (Win/Mac) | nhánh | commit | sửa gì (một câu tiếng Việt)`

Lý do có nếp này: mã chưa đẩy ở một máy mà máy kia sửa cùng chỗ thì gộp lại rất đau. Một dòng nhật ký rẻ hơn nhiều so với một buổi gỡ xung đột.
