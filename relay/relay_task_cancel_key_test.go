package relay

import "testing"

// Kênh RunningHub nhiều key: huỷ phải dùng đúng key đã gửi tác vụ, không thì RunningHub báo APIKEY_TASK_NOT_FOUND.
func TestCancelKeyUsesTheSubmittingKey(t *testing.T) {
	if got := cancelKey("key-a\nkey-b", "key-b"); got != "key-b" {
		t.Fatalf("want the task's own key, got %q", got)
	}
	if got := cancelKey("key-a", ""); got != "key-a" {
		t.Fatalf("want the channel key when the task has none, got %q", got)
	}
}
