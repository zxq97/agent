package tyche

// fixDateTimeSeconds 用字节扫描把 JSON 里所有 "YYYY-MM-DD HH:MM"(不带秒)的时间字符串补成
// "YYYY-MM-DD HH:MM:00"。只匹配缺秒的情况,已有秒位的不重复追加。
// 避免 import regexp。
func fixDateTimeSeconds(s string) string {
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i])
		// 检测 out 末尾 16 字节是否为 "YYYY-MM-DD HH:MM",且下一字节是双引号(JSON 串结束)
		if i+1 < len(s) && s[i+1] == '"' && len(out) >= 16 {
			tail := string(out[len(out)-16:])
			if isDateTimeWithoutSec(tail) {
				out = append(out, ':', '0', '0')
			}
		}
	}
	return string(out)
}

// isDateTimeWithoutSec 判断 16 字节是否符合 "YYYY-MM-DD HH:MM"。
func isDateTimeWithoutSec(s string) bool {
	if len(s) != 16 {
		return false
	}
	return isDigits(s[0:4]) && s[4] == '-' &&
		isDigits(s[5:7]) && s[7] == '-' &&
		isDigits(s[8:10]) && s[10] == ' ' &&
		isDigits(s[11:13]) && s[13] == ':' &&
		isDigits(s[14:16])
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
