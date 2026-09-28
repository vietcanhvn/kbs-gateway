package setting

// payOS (payos.vn): chuyển khoản ngân hàng qua mã VietQR, payOS báo về webhook
// khi tiền vào tài khoản nên nạp tiền tự động.
var PayOSEnabled = false
var PayOSClientId = ""
var PayOSApiKey = ""
var PayOSChecksumKey = ""

// PayOSUnitPrice là số VND cho 1 đơn vị nạp (1 USD hạn mức khi hiển thị theo USD).
// 0 = chưa cài: cổng không bật cho tới khi quản trị điền tỉ giá.
var PayOSUnitPrice = 0.0
var PayOSMinTopUp = 1
