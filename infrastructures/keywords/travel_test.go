package keywords

import "testing"

func TestParseTravelConstraints_ModeAfter(t *testing.T) {
	afe := NewAdvancedFieldExtractor()
	text := "离七宝万科广场步行10分钟"
	constraints := afe.ParseTravelConstraints(text, TravelSpeeds{})
	if len(constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(constraints))
	}
	c := constraints[0]
	if c.AnchorRaw != "七宝万科广场" {
		t.Fatalf("unexpected anchor: %+v", c)
	}
	if c.Mode != ModeWalk {
		t.Fatalf("expected walk mode, got %s", c.Mode)
	}
	if c.Duration.MinMinutes != 10 || c.Duration.MaxMinutes != 10 {
		t.Fatalf("unexpected duration: %+v", c.Duration)
	}
	if c.RadiusMinMeters <= 0 || c.RadiusMaxMeters <= 0 {
		t.Fatalf("radius not computed: %+v", c)
	}
}

func TestParseTravelConstraints_ModeBefore(t *testing.T) {
	afe := NewAdvancedFieldExtractor()
	text := "骑车到人民公园5-10分钟"
	constraints := afe.ParseTravelConstraints(text, TravelSpeeds{WalkMpm: 100, BikeMpm: 200, DriveMpm: 400})
	if len(constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(constraints))
	}
	c := constraints[0]
	if c.Mode != ModeBike {
		t.Fatalf("expected bike mode, got %s", c.Mode)
	}
	if c.Duration.MinMinutes != 5 || c.Duration.MaxMinutes != 10 {
		t.Fatalf("unexpected duration: %+v", c.Duration)
	}
	if c.RadiusMinMeters != 1000 || c.RadiusMaxMeters != 2000 {
		t.Fatalf("unexpected radius: min=%v max=%v", c.RadiusMinMeters, c.RadiusMaxMeters)
	}
}

func TestParseTravelConstraints_HourUnits(t *testing.T) {
	afe := NewAdvancedFieldExtractor()
	text := "步行到世纪公园0.5小时"
	constraints := afe.ParseTravelConstraints(text, TravelSpeeds{})
	if len(constraints) != 1 {
		t.Fatalf("expected 1 constraint, got %d", len(constraints))
	}
	c := constraints[0]
	if c.Duration.MinMinutes != 30 || c.Duration.MaxMinutes != 30 {
		t.Fatalf("expected 30 minutes duration, got %+v", c.Duration)
	}
}

func TestParseTravelConstraints_Invalid(t *testing.T) {
	afe := NewAdvancedFieldExtractor()
	text := "这个位置很近"
	constraints := afe.ParseTravelConstraints(text, TravelSpeeds{})
	if len(constraints) != 0 {
		t.Fatalf("expected no constraints, got %d", len(constraints))
	}
}
