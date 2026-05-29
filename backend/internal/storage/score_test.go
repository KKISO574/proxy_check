package storage

import "testing"

func TestScoreNodeIncludesMiaoSpeedBandwidthWhenPresent(t *testing.T) {
	bandwidth := 120.0
	node := Node{
		Status: "available",
		Metrics: map[string]MetricSummary{
			"miaospeed_bandwidth": {
				Metric:  "miaospeed_bandwidth",
				Value:   &bandwidth,
				Success: true,
			},
		},
	}

	score, confidence, breakdown := ScoreNode(node)
	if score == nil {
		t.Fatalf("expected score")
	}
	if component, ok := breakdown["bandwidth"]; !ok {
		t.Fatalf("expected bandwidth component, got %#v", breakdown)
	} else if component.Score != 100 || component.Value == nil || *component.Value != bandwidth {
		t.Fatalf("unexpected bandwidth component: %#v", component)
	}
	if confidence <= 0.1 {
		t.Fatalf("expected bandwidth to increase confidence above status-only, got %v", confidence)
	}
}

func TestScoreNodePenalizesDNSLeakWhenMetaIndicatesLeak(t *testing.T) {
	clean := "clean"
	leaked := "leaked"
	cleanNode := Node{
		Status:  "available",
		Metrics: map[string]MetricSummary{},
		Meta:    &NodeMeta{DNSLeak: &clean},
	}
	leakedNode := Node{
		Status:  "available",
		Metrics: map[string]MetricSummary{},
		Meta:    &NodeMeta{DNSLeak: &leaked},
	}

	cleanScore, _, cleanBreakdown := ScoreNode(cleanNode)
	leakedScore, _, leakedBreakdown := ScoreNode(leakedNode)
	if cleanScore == nil || leakedScore == nil {
		t.Fatalf("expected scores")
	}
	if cleanBreakdown["dns_leak"].Score != 100 {
		t.Fatalf("expected clean dns score, got %#v", cleanBreakdown["dns_leak"])
	}
	if leakedBreakdown["dns_leak"].Score != 0 || leakedBreakdown["dns_leak"].Status != "leaked" {
		t.Fatalf("expected leaked dns penalty, got %#v", leakedBreakdown["dns_leak"])
	}
	if *leakedScore >= *cleanScore {
		t.Fatalf("expected leaked score %v to be below clean score %v", *leakedScore, *cleanScore)
	}
}

func TestScoreNodeIncludesUnlockStatusModifier(t *testing.T) {
	netflix := "full"
	disney := "blocked"
	openai := "available"
	youtube := "JP"
	node := Node{
		Status:  "available",
		Metrics: map[string]MetricSummary{},
		Meta: &NodeMeta{
			NetflixUnlock: &netflix,
			DisneyUnlock:  &disney,
			OpenAIUnlock:  &openai,
			YouTubeUnlock: &youtube,
		},
	}

	score, confidence, breakdown := ScoreNode(node)
	if score == nil {
		t.Fatalf("expected score")
	}
	component, ok := breakdown["unlock"]
	if !ok {
		t.Fatalf("expected unlock component, got %#v", breakdown)
	}
	if component.Score != 75 || component.Status != "3/4" {
		t.Fatalf("unexpected unlock component: %#v", component)
	}
	if confidence <= 0.1 {
		t.Fatalf("expected unlock to increase confidence above status-only, got %v", confidence)
	}
}
