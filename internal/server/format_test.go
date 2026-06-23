package server

import "testing"

func TestThousands(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"zero", 0, "0"},
		{"hundreds", 817, "817"},
		{"thousands", 10000, "10,000"},
		{"cap", 50000, "50,000"},
		{"total", 55723, "55,723"},
		{"million", 1234567, "1,234,567"},
		{"int64", int64(55723), "55,723"},
		{"int32", int32(2048), "2,048"},
		{"negative", -12345, "-12,345"},
		{"other type falls back", "n/a", "n/a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := thousands(tc.in); got != tc.want {
				t.Errorf("thousands(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
