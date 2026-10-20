package model

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func annotationFixture() DashboardAnnotation {
	now := NowUTC()
	return DashboardAnnotation{ID: "a1", DashboardID: PublicDashboardID, PanelID: "cpu", Title: "Deploy 1.2",
		Text: "rolled out", Tags: []string{"deploy"}, Color: "blue", Visibility: AnnotationVisibilityPublic,
		From: now.Add(-time.Hour), To: now}
}

func TestValidateDashboardAnnotationAcceptsBoundedInput(t *testing.T) {
	if err := ValidateDashboardAnnotation(annotationFixture()); err != nil {
		t.Fatalf("valid annotation rejected: %v", err)
	}
	node := annotationFixture()
	node.DashboardID = "node-11111111-2222-3333-4444-555555555555"
	if err := ValidateDashboardAnnotation(node); err != nil {
		t.Fatalf("node template annotation rejected: %v", err)
	}
}

func TestValidateDashboardAnnotationRejectsInvalidFields(t *testing.T) {
	cases := map[string]func(value *DashboardAnnotation){
		"dashboard":  func(value *DashboardAnnotation) { value.DashboardID = "" },
		"panel":      func(value *DashboardAnnotation) { value.PanelID = "bad panel" },
		"reserved":   func(value *DashboardAnnotation) { value.PanelID = "__proto__" },
		"title":      func(value *DashboardAnnotation) { value.Title = " " },
		"long title": func(value *DashboardAnnotation) { value.Title = strings.Repeat("a", 201) },
		"long text":  func(value *DashboardAnnotation) { value.Text = strings.Repeat("a", 2100) },
		"tags":       func(value *DashboardAnnotation) { value.Tags = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} },
		"duplicate":  func(value *DashboardAnnotation) { value.Tags = []string{"deploy", "deploy"} },
		"tag":        func(value *DashboardAnnotation) { value.Tags = []string{"Deploy"} },
		"color":      func(value *DashboardAnnotation) { value.Color = "chartreuse" },
		"visibility": func(value *DashboardAnnotation) { value.Visibility = "shared" },
		"empty time": func(value *DashboardAnnotation) { value.From = time.Time{} },
		"reversed":   func(value *DashboardAnnotation) { value.From, value.To = value.To, value.From },
		"long": func(value *DashboardAnnotation) {
			value.To = value.From.Add(time.Duration(MaxAnnotationWindowSeconds+1) * time.Second)
		},
	}
	for name, mutate := range cases {
		value := annotationFixture()
		mutate(&value)
		if err := ValidateDashboardAnnotation(value); !errors.Is(err, ErrInvalidDashboardAnnotation) {
			t.Fatalf("%s: error = %v, want ErrInvalidDashboardAnnotation", name, err)
		}
	}
}

func TestValidateDashboardSnapshotAndLibraryPanel(t *testing.T) {
	definition := DefaultMetricDashboard("personal").Definition
	snapshot := DashboardSnapshot{DashboardID: "dash-1", Name: "Freeze", Revision: 1, Definition: definition}
	if err := ValidateDashboardSnapshot(snapshot); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	snapshot.Revision = 0
	if err := ValidateDashboardSnapshot(snapshot); !errors.Is(err, ErrInvalidDashboardSnapshot) {
		t.Fatalf("zero revision error = %v", err)
	}
	snapshot.Revision, snapshot.Name = 1, ""
	if err := ValidateDashboardSnapshot(snapshot); !errors.Is(err, ErrInvalidDashboardSnapshot) {
		t.Fatalf("empty name error = %v", err)
	}
	snapshot.Name, snapshot.DashboardID = "Freeze", "bad id"
	if err := ValidateDashboardSnapshot(snapshot); !errors.Is(err, ErrInvalidDashboardSnapshot) {
		t.Fatalf("bad dashboard error = %v", err)
	}
	snapshot.DashboardID, snapshot.Definition.Title = "dash-1", ""
	if err := ValidateDashboardSnapshot(snapshot); !errors.Is(err, ErrInvalidDashboardSnapshot) {
		t.Fatalf("invalid definition error = %v", err)
	}
}

func TestValidateLibraryPanelUsesPanelRules(t *testing.T) {
	panel := DefaultMetricDashboard("personal").Definition.Panels[0]
	if err := ValidateLibraryPanel(DashboardLibraryPanel{Name: "CPU", Panel: panel}); err != nil {
		t.Fatalf("valid library panel rejected: %v", err)
	}
	if err := ValidateLibraryPanel(DashboardLibraryPanel{Name: "", Panel: panel}); !errors.Is(err, ErrInvalidLibraryPanel) {
		t.Fatalf("empty name error = %v", err)
	}
	if err := ValidateLibraryPanel(DashboardLibraryPanel{Name: "CPU", Description: strings.Repeat("a", 513), Panel: panel}); !errors.Is(err, ErrInvalidLibraryPanel) {
		t.Fatalf("long description error = %v", err)
	}
	if err := ValidateLibraryPanel(DashboardLibraryPanel{Name: "CPU", Panel: DashboardPanel{ID: "x"}}); !errors.Is(err, ErrInvalidLibraryPanel) {
		t.Fatalf("invalid panel error = %v", err)
	}
}

func TestValidateDashboardPanelDefinitionResolvesVariables(t *testing.T) {
	panel := DefaultMetricDashboard("personal").Definition.Panels[0]
	panel.Series[0].NodeID = "$node"
	if err := ValidateDashboardPanelDefinition(panel); err != nil {
		t.Fatalf("variable series rejected: %v", err)
	}
	panel.Series[0].NodeID = "$Bad-Name"
	if err := ValidateDashboardPanelDefinition(panel); !errors.Is(err, ErrInvalidLibraryPanel) {
		t.Fatalf("bad variable error = %v", err)
	}
	if err := ValidateLibraryPanel(DashboardLibraryPanel{Name: "CPU", Panel: panel}); !errors.Is(err, ErrInvalidLibraryPanel) {
		t.Fatal("library validation must surface the panel error")
	}
}

func TestPanelAlertPhaseRankOrdersSeverity(t *testing.T) {
	ranked := []struct{ phase, status string }{{AlertPhaseNormal, EvaluationOK}, {AlertPhaseRecovering, EvaluationOK},
		{AlertPhaseNormal, EvaluationNoData}, {AlertPhaseNormal, EvaluationError}, {AlertPhasePending, EvaluationOK},
		{AlertPhaseFiring, EvaluationOK}}
	for index := 1; index < len(ranked); index++ {
		previous, current := ranked[index-1], ranked[index]
		if PanelAlertPhaseRank(previous.phase, previous.status) >= PanelAlertPhaseRank(current.phase, current.status) {
			t.Fatalf("rank(%v) must be below rank(%v)", previous, current)
		}
	}
	if PanelAlertPhaseRank("unknown", "unknown") != 0 {
		t.Fatal("unknown phases must rank lowest")
	}
}
