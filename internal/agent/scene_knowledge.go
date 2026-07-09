package agent

import (
	"strings"

	"github.com/zxq97/rental-agent/internal/types"
)

type SceneRule struct {
	Match      []string
	NeedType   string
	Value      string
	Confidence float64
	Tip        string
}

type SceneKnowledgePatch struct {
	Needs []types.NeedDelta
	Tip   string
}

var sceneRules = []SceneRule{
	{Match: []string{"老人", "小孩"}, NeedType: "vehicle_type", Value: "SUV", Confidence: 0.65, Tip: "带老人小孩更建议看空间和上下车便利性。"},
	{Match: []string{"家庭", "出游"}, NeedType: "vehicle_type", Value: "SUV", Confidence: 0.6, Tip: "家庭出游可以优先看空间更宽裕的车。"},
	{Match: []string{"商务", "接送"}, NeedType: "vehicle_type", Value: "商务车", Confidence: 0.65, Tip: "商务接送更适合空间和乘坐感稳一点的车型。"},
	{Match: []string{"情侣", "自驾"}, NeedType: "vehicle_type", Value: "轿车", Confidence: 0.6, Tip: "两人自驾可以优先看好开省心的轿车。"},
	{Match: []string{"接送机", "机场"}, NeedType: "vehicle_type", Value: "SUV", Confidence: 0.6, Tip: "接送机通常行李多,可以优先看后备厢更宽裕的车。"},
}

func RenderSceneKB() string {
	var b strings.Builder
	b.WriteString("场景知识库(scene KB):\n")
	for _, r := range sceneRules {
		b.WriteString("- match=")
		b.WriteString(strings.Join(r.Match, "|"))
		b.WriteString(" => ADD ")
		b.WriteString(r.NeedType)
		b.WriteString("=")
		b.WriteString(r.Value)
		b.WriteString("(soft); tip=")
		b.WriteString(r.Tip)
		b.WriteString("\n")
	}
	return b.String()
}

func MatchSceneKnowledge(text string) SceneKnowledgePatch {
	for _, r := range sceneRules {
		if sceneRuleMatches(text, r.Match) {
			return SceneKnowledgePatch{
				Needs: []types.NeedDelta{{
					Op:         "ADD",
					Type:       r.NeedType,
					Value:      r.Value,
					Hardness:   "soft",
					Confidence: r.Confidence,
				}},
				Tip: r.Tip,
			}
		}
	}
	return SceneKnowledgePatch{}
}

func sceneRuleMatches(text string, keys []string) bool {
	for _, k := range keys {
		if !strings.Contains(text, k) {
			return false
		}
	}
	return true
}

func ApplySceneKnowledgeToDecision(dec *Decision, userText string) SceneKnowledgePatch {
	if dec == nil || dec.Tool != ToolSearchVehicles {
		return SceneKnowledgePatch{}
	}
	patch := MatchSceneKnowledge(userText)
	if len(patch.Needs) == 0 {
		return patch
	}
	dec.NeedDelta = append(patch.Needs, dec.NeedDelta...)
	return patch
}
