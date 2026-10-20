package schema

import "testing"

func TestArrangementIssueRequiresCompletePhraseCoverage(t *testing.T) {
	plan := ArrangementPlan{Title: "山间听弦", Tempo: 80, TimeSignature: []int{4, 4}, Seconds: 90, Sections: []ArrangementSection{
		{Name: "前奏", StartBar: 1, EndBar: 4, Phrases: []ArrangementPhrase{
			{Name: "起句", StartBar: 1, EndBar: 2, Parts: []ArrangementPart{{Instrument: "箫", Role: "melody", Source: "real"}}},
			{Name: "答句", StartBar: 3, EndBar: 4, Parts: []ArrangementPart{{Instrument: "琵琶", Role: "melody", Technique: "挑弹", Source: "ai"}}},
		}},
	}}
	if issue := ArrangementIssue(&plan); issue != "" {
		t.Fatalf("valid plan rejected: %s", issue)
	}
	plan.Sections[0].Phrases[1].StartBar = 4
	if issue := ArrangementIssue(&plan); issue == "" {
		t.Fatal("gap between phrases accepted")
	}
	plan.Sections[0].Phrases[1].StartBar = 3
	plan.Sections[0].Phrases[1].Parts[0].Source = "missing"
	if issue := ArrangementIssue(&plan); issue == "" {
		t.Fatal("invalid source accepted")
	}
}

func TestEnsureArrangementIDsAndRejectDuplicates(t *testing.T) {
	plan := ArrangementPlan{Title: "测试", Tempo: 120, TimeSignature: []int{4, 4}, Seconds: 4, Sections: []ArrangementSection{{Name: "A", StartBar: 1, EndBar: 2, Phrases: []ArrangementPhrase{{Name: "一句", StartBar: 1, EndBar: 2, Parts: []ArrangementPart{{Instrument: "箫", Role: "melody", Source: "real"}, {Instrument: "琵琶", Role: "other", Source: "ai"}}}}}}}
	EnsureArrangementIDs(&plan)
	section := plan.Sections[0]
	phrase := section.Phrases[0]
	if issue := ArrangementIssue(&plan); issue != "" || !ValidID(section.ID) || !ValidID(phrase.ID) || !ValidID(phrase.Parts[0].ID) || !ValidID(phrase.Parts[1].ID) {
		t.Fatalf("服务端生成的身份无效: issue=%s plan=%+v", issue, plan)
	}
	plan.Sections[0].Phrases[0].Parts[1].ID = phrase.Parts[0].ID
	if issue := ArrangementIssue(&plan); issue == "" {
		t.Fatal("编曲计划不能包含重复 partId")
	}
}
