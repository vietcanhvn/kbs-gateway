# ⛔ Nhánh `main` là bản CŨ — không triển khai, không sửa

**Bản thật đang chạy nằm ở nhánh `studio`.**

Nhánh `main` này lần cuối có commit ngày **15/09/2026**, thiếu **27 commit** so với `studio` (giá theo mô hình, email, hoa hồng, PayOS và nhiều thứ khác). Triển khai bản này là **mất tính năng**.

## Nếu bạn là agent vừa mở repo và đang đọc dòng này

Bạn đang ở nhầm nhánh. Làm thế này:

```
git checkout studio
git pull
```

Rồi đọc `CLAUDE.md` và 5 dòng cuối `NHAT_KY_MA.md` trên nhánh `studio` trước khi sửa gì.

## Vì sao có tệp này

Ngày 04/10/2026 có **hai lần dev lấy nhầm nhánh `main`** rồi phải làm lại từ đầu. Một lần là tôi: tôi sửa màn đăng nhập trên `main`, build sạch, mở pull request, rồi mới phát hiện bản đó thiếu 27 commit và không dùng được. Phải cherry-pick lại sang `studio`.

Tệp này để người sau không mất thời gian như vậy nữa.

## Một khả năng chưa xác minh

`main` **có thể** đang được dùng để kéo cập nhật từ dự án gốc `QuantumNous/new-api` (`go.mod` khai `module github.com/QuantumNous/new-api`). Nếu đúng vậy thì **đừng xoá nhánh này** — nó vẫn có việc của nó, chỉ là không phải nhánh để làm việc hay triển khai. Tôi không xác minh được điều này từ phía máy, nhờ người biết rõ xác nhận và sửa lại dòng này.
