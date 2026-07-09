package tyche

import "testing"

func TestFixDateTimeSeconds(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			`{"date_time":"2026-06-15 18:00"}`,
			`{"date_time":"2026-06-15 18:00:00"}`,
		},
		{
			// 已有秒位,不重复追加
			`{"date_time":"2026-06-15 18:00:00"}`,
			`{"date_time":"2026-06-15 18:00:00"}`,
		},
		{
			// 多个时间
			`{"a":"2026-01-02 09:30","b":"2026-12-31 23:59"}`,
			`{"a":"2026-01-02 09:30:00","b":"2026-12-31 23:59:00"}`,
		},
		{
			// 非时间字符串不动
			`{"name":"首都机场"}`,
			`{"name":"首都机场"}`,
		},
		{
			`{}`,
			`{}`,
		},
	}
	for _, c := range cases {
		if got := fixDateTimeSeconds(c.in); got != c.want {
			t.Errorf("fixDateTimeSeconds(%q)\n  = %q\n want %q", c.in, got, c.want)
		}
	}
}

func TestIsDateTimeWithoutSec(t *testing.T) {
	if !isDateTimeWithoutSec("2026-06-15 18:00") {
		t.Error("should match YYYY-MM-DD HH:MM")
	}
	if isDateTimeWithoutSec("2026-06-15 18:00:00") {
		t.Error("19-char with sec should not match 16-char form")
	}
	if isDateTimeWithoutSec("not a datetime!!") {
		t.Error("garbage should not match")
	}
}
