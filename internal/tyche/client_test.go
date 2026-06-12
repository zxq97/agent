package tyche

import (
	"testing"
)

func TestFixDateTimeSeconds(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "缺秒位,补 :00",
			in:   `{"date_time":"2026-06-15 18:00"}`,
			want: `{"date_time":"2026-06-15 18:00:00"}`,
		},
		{
			name: "已有秒位,不重复追加",
			in:   `{"date_time":"2026-06-15 18:00:00"}`,
			want: `{"date_time":"2026-06-15 18:00:00"}`,
		},
		{
			name: "多个 date_time 字段都补秒",
			in:   `{"pickup":{"date_time":"2026-06-13 09:00"},"dropoff":{"date_time":"2026-06-15 18:00"}}`,
			want: `{"pickup":{"date_time":"2026-06-13 09:00:00"},"dropoff":{"date_time":"2026-06-15 18:00:00"}}`,
		},
		{
			name: "一个缺秒一个不缺",
			in:   `{"a":"2026-06-13 09:00","b":"2026-06-15 18:00:00"}`,
			want: `{"a":"2026-06-13 09:00:00","b":"2026-06-15 18:00:00"}`,
		},
		{
			name: "非时间字符串不受影响",
			in:   `{"name":"北京南站","city_id":1}`,
			want: `{"name":"北京南站","city_id":1}`,
		},
		{
			name: "空字符串不崩溃",
			in:   `{}`,
			want: `{}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := fixDateTimeSeconds(c.in)
			if got != c.want {
				t.Errorf("fixDateTimeSeconds(%q)\n  got  = %q\n  want = %q", c.in, got, c.want)
			}
		})
	}
}
