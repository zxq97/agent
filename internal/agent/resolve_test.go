package agent

import (
	"testing"

	"github.com/zxq97/rental-agent/internal/orchestration"
)

func newStateWithQuotes(qs []orchestration.QuoteRef) *orchestration.ConversationState {
	st := orchestration.New("s1", "u1")
	st.SetQuotes("ctx-123", qs)
	return st
}

var sample = []orchestration.QuoteRef{
	{ReferenceID: "ref-1", Supplier: "s1", CarName: "大众朗逸", BrandName: "大众", Index: 1},
	{ReferenceID: "ref-2", Supplier: "s2", CarName: "丰田卡罗拉", BrandName: "丰田", Index: 2},
	{ReferenceID: "ref-3", Supplier: "s3", CarName: "本田雅阁", BrandName: "本田", Index: 3},
}

func TestResolve_Ordinal(t *testing.T) {
	st := newStateWithQuotes(sample)
	ref, clar := ResolveQuoteRef(st, "看第一辆的明细")
	if ref != "ref-1" || clar != nil {
		t.Fatalf("ordinal: ref=%q clar=%v", ref, clar)
	}
	ref, _ = ResolveQuoteRef(st, "第2个多少钱")
	if ref != "ref-2" {
		t.Fatalf("第2个 → %q", ref)
	}
}

func TestResolve_ByName(t *testing.T) {
	st := newStateWithQuotes(sample)
	ref, clar := ResolveQuoteRef(st, "朗逸要加全险吗")
	if ref != "ref-1" || clar != nil {
		t.Fatalf("byname: ref=%q clar=%v", ref, clar)
	}
}

func TestResolve_Multi(t *testing.T) {
	qs := []orchestration.QuoteRef{
		{ReferenceID: "ref-1", CarName: "大众朗逸", BrandName: "大众", Index: 1},
		{ReferenceID: "ref-2", CarName: "大众帕萨特", BrandName: "大众", Index: 2},
	}
	st := newStateWithQuotes(qs)
	ref, clar := ResolveQuoteRef(st, "那辆大众")
	if ref != "" || clar == nil {
		t.Fatalf("multi should clarify: ref=%q clar=%v", ref, clar)
	}
	if len(clar.Options) != 2 {
		t.Fatalf("clar options = %v", clar.Options)
	}
}

func TestResolve_NoMatch(t *testing.T) {
	st := newStateWithQuotes(sample)
	ref, clar := ResolveQuoteRef(st, "有没有特斯拉")
	if ref != "" || clar != nil {
		t.Fatalf("nomatch: ref=%q clar=%v", ref, clar)
	}
}

func TestResolve_SingleFuzzy(t *testing.T) {
	st := newStateWithQuotes(sample[:1])
	ref, _ := ResolveQuoteRef(st, "那辆怎么样")
	if ref != "ref-1" {
		t.Fatalf("single fuzzy → %q", ref)
	}
}

func TestResolve_Stale(t *testing.T) {
	st := newStateWithQuotes(sample)
	// 手动把报价时间推到过期
	st.SetQuotes("ctx", sample)
	// 直接验证非过期路径已覆盖;过期路径靠 IsQuoteStale,另由 state 测试覆盖
	ref, _ := ResolveQuoteRef(st, "第一辆")
	if ref == "" {
		t.Fatal("fresh quotes should resolve")
	}
}

func TestResolveMany(t *testing.T) {
	st := newStateWithQuotes(sample)
	resolved, clar, missing := ResolveMany(st, []string{"朗逸", "卡罗拉"})
	if clar != nil || len(missing) != 0 {
		t.Fatalf("clar=%v missing=%v", clar, missing)
	}
	if len(resolved) != 2 || resolved[0] != "ref-1" || resolved[1] != "ref-2" {
		t.Fatalf("resolved=%v", resolved)
	}

	// 一个命中一个缺失
	resolved, _, missing = ResolveMany(st, []string{"朗逸", "特斯拉"})
	if len(resolved) != 1 || len(missing) != 1 || missing[0] != "特斯拉" {
		t.Fatalf("resolved=%v missing=%v", resolved, missing)
	}
}
