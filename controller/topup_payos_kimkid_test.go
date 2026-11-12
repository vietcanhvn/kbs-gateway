package controller

import "testing"

func TestKimkidPayOSOrderCode(t *testing.T) {
	cases := map[string]string{
		`{"data":{"orderCode":77000000000001}}`:   "77000000000001",
		`{"data":{"orderCode":"77123456789012"}}`: "77123456789012",
		`{"data":{"orderCode":1762950000000123}}`: "",
		`{"data":{"orderCode":7700000000001}}`:    "",
		`{"data":{"orderCode":770000000000012}}`:  "",
		`{"data":{"orderCode":123}}`:              "",
		`{"data":{}}`:                             "",
		`not json`:                                "",
	}
	for body, want := range cases {
		if got := kimkidPayOSOrderCode([]byte(body)); got != want {
			t.Errorf("%s: got %q want %q", body, got, want)
		}
	}
}
