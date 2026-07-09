package agent

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

// 圆圈序号(①~⑨)独立表,不会跟"第1"这类子串混淆。
var circledDigits = map[rune]int{
	'①': 1, '②': 2, '③': 3, '④': 4, '⑤': 5, '⑥': 6, '⑦': 7, '⑧': 8, '⑨': 9,
}

// 中文数字 → int(1~10),用于识别"第一/二/三..十辆"。
var cnDigits = map[string]int{
	"一": 1, "二": 2, "三": 3, "四": 4, "五": 5,
	"六": 6, "七": 7, "八": 8, "九": 9, "十": 10,
}

// 序数正则:匹配 "第<X>[辆|个]?" 或 "<X>号",其中 X 是 1~2 位阿拉伯数字或单个中文数字。
// 允许 X 前后有空白/全角空格。用 (?:) 非捕获组把量词与主体分开,提取时只取 X。
//
// 例:第2辆 / 第 2 辆 / 第2 / 第一辆 / 一号 / 2号 / 第十辆
//
// 反例(不误匹配):
//   - "第10辆什么价" 里的 "10" 会整段命中,不会退化到 "1"
//   - "20块钱" 之类不带"第/号"前后缀的裸数字不会命中
var ordinalRe = regexp.MustCompile(`第\s*([一二三四五六七八九十]|\d{1,2})\s*(?:辆|个)?|(\d{1,2})\s*号|([一二三四五六七八九十])号`)

// parseOrdinal 从用户文本提取序数(1-based)。返回 0 表示没找到。
// 优先级:
//  1. 圆圈符号(①②③...)—— 独立字符不会误撞
//  2. "第 X" / "X 号" 正则(数字优先长匹配,不会有子串问题)
func parseOrdinal(text string) int {
	for _, r := range text {
		if n, ok := circledDigits[r]; ok {
			return n
		}
	}
	m := ordinalRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	// 三个捕获组:第X / X号 / 中文X号 —— 谁非空取谁
	for _, g := range m[1:] {
		if g == "" {
			continue
		}
		if n, ok := cnDigits[g]; ok {
			return n
		}
		if n, err := strconv.Atoi(g); err == nil {
			return n
		}
	}
	return 0
}

// ResolveQuoteRef 把"第一辆/朗逸/那辆 SUV"翻译成 reference_id。纯 Go,不调 LLM。
//
// 返回三种状态:
//   - 命中 1 个:ref != "", clarify == nil
//   - 命中多个:ref == "", clarify != nil(让用户选)
//   - 命中 0 个:ref == "", clarify == nil(调用方降级:引导重搜/追问)
//
// 报价过期(超 QuoteTTL)直接返回空,调用方应触发重搜。
func ResolveQuoteRef(state *orchestration.ConversationState, userText string) (ref string, clarify *Clarification) {
	if state.IsQuoteStale(tools.QuoteTTL) {
		return "", nil
	}
	_, quotes, _ := state.SnapshotQuotes()
	if len(quotes) == 0 {
		return "", nil
	}
	matches := matchQuotes(userText, quotes)
	switch len(matches) {
	case 1:
		return matches[0].ReferenceID, nil
	case 0:
		return "", nil
	default:
		return "", buildRefClarification(matches)
	}
}

// ResolveMany 解析多个指代(车型对比用)。逐个 resolve,返回命中的 ref 列表 +
// 第一个遇到的多义澄清(若有)。missing 收集 0 命中的原始指代词。
func ResolveMany(state *orchestration.ConversationState, refs []string) (resolved []string, clarify *Clarification, missing []string) {
	for _, r := range refs {
		ref, c := ResolveQuoteRef(state, r)
		if c != nil {
			return resolved, c, missing
		}
		if ref == "" {
			missing = append(missing, r)
			continue
		}
		resolved = append(resolved, ref)
	}
	return resolved, nil, missing
}

// matchQuotes 匹配规则(按优先级):
//  1. 序号(parseOrdinal:第X辆/X号/圆圈符号,数字最长匹配,不会把"第10"误当"第1")
//     命中的 Index 若越界(如只有 2 辆却问"第10"),视为未命中,不误落到最后一辆
//  2. 车名/品牌精确包含:"朗逸"/"大众 朗逸" → CarName/BrandName
//  3. (单候选兜底)只有一辆报价时,模糊指代("那辆""这个")直接命中
func matchQuotes(text string, quotes []orchestration.QuoteRef) []orchestration.QuoteRef {
	t := strings.TrimSpace(text)

	// 1. 序号
	if idx := parseOrdinal(t); idx > 0 {
		for _, q := range quotes {
			if q.Index == idx {
				return []orchestration.QuoteRef{q}
			}
		}
		// idx 越界 → 明确未命中,不继续走车名/品牌分支(避免"第10辆"里的"10"再被别处误抓)
		return nil
	}

	// 2. 车名 / 品牌 / 车型(车名去掉品牌前缀)包含
	var byName []orchestration.QuoteRef
	for _, q := range quotes {
		if q.CarName != "" && strings.Contains(t, q.CarName) {
			byName = append(byName, q)
			continue
		}
		// 车型部分:CarName 去掉 BrandName 前缀(如 "大众朗逸"→"朗逸"),用户常只说车型
		if model := modelPart(q); model != "" && strings.Contains(t, model) {
			byName = append(byName, q)
			continue
		}
		if q.BrandName != "" && strings.Contains(t, q.BrandName) {
			byName = append(byName, q)
		}
	}
	if len(byName) > 0 {
		return byName
	}

	// 3. 单候选兜底:只有一辆时,"那辆/这个/它" 等模糊指代直接命中
	if len(quotes) == 1 {
		for _, w := range []string{"那辆", "这辆", "那个", "这个", "它", "这款", "那款"} {
			if strings.Contains(t, w) {
				return []orchestration.QuoteRef{quotes[0]}
			}
		}
	}

	return nil
}

// modelPart 取车名去掉品牌前缀的车型部分(如 CarName="大众朗逸" BrandName="大众" → "朗逸")。
// 仅当 CarName 以 BrandName 开头且剩余非空时返回,否则返回空。
func modelPart(q orchestration.QuoteRef) string {
	if q.BrandName == "" || q.CarName == "" {
		return ""
	}
	if rest := strings.TrimPrefix(q.CarName, q.BrandName); rest != q.CarName {
		return strings.TrimSpace(rest)
	}
	return ""
}

// buildRefClarification 多义时构造澄清反问,选项用"序号 车名"。
func buildRefClarification(matches []orchestration.QuoteRef) *Clarification {
	opts := make([]string, 0, len(matches))
	for _, q := range matches {
		label := q.CarName
		if label == "" {
			label = q.BrandName
		}
		opts = append(opts, label)
	}
	return &Clarification{
		Question: "你是想看哪一辆呢?",
		Options:  opts,
		Slot:     "vehicle_ref",
	}
}
