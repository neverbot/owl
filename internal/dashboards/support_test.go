package dashboards

import "testing"

// fakeCaps is a controllable Capabilities for tests.
type fakeCaps struct {
	unsupported map[string]string // expr -> reason; missing keys are "supported"
}

func (f *fakeCaps) IsSupported(expr string) (bool, string) {
	if reason, bad := f.unsupported[expr]; bad {
		return false, reason
	}
	return true, ""
}

func panelWith(typ string, exprs ...string) Panel {
	p := Panel{Type: typ}
	for _, e := range exprs {
		p.Targets = append(p.Targets, Target{Expr: e})
	}
	return p
}

func TestAnnotateSupport_UnsupportedPanelType(t *testing.T) {
	panels := []Panel{panelWith("heatmap", "some_metric")}
	caps := &fakeCaps{}
	out := annotateSupport(panels, caps)

	if out[0].Support.Status != "unsupported" {
		t.Errorf("Status = %q, want %q", out[0].Support.Status, "unsupported")
	}
	if out[0].Support.Reason == "" {
		t.Error("Reason must be non-empty for unsupported panel type")
	}
}

func TestAnnotateSupport_AllTargetsSupported(t *testing.T) {
	panels := []Panel{panelWith("timeseries", "owl_runtime_goroutines", "owl_runtime_alloc_bytes")}
	caps := &fakeCaps{} // everything supported
	out := annotateSupport(panels, caps)

	if out[0].Support.Status != "supported" {
		t.Errorf("Status = %q, want %q", out[0].Support.Status, "supported")
	}
	if out[0].Support.Reason != "" {
		t.Errorf("Reason should be empty for supported panel, got %q", out[0].Support.Reason)
	}
}

func TestAnnotateSupport_OneTargetUnsupported(t *testing.T) {
	panels := []Panel{panelWith("stat", "owl_runtime_goroutines", "rate(http_requests_total[5m])")}
	caps := &fakeCaps{
		unsupported: map[string]string{
			"rate(http_requests_total[5m])": "rate() not supported",
		},
	}
	out := annotateSupport(panels, caps)

	if out[0].Support.Status != "unsupported" {
		t.Errorf("Status = %q, want %q", out[0].Support.Status, "unsupported")
	}
	if out[0].Support.Reason != "rate() not supported" {
		t.Errorf("Reason = %q, want %q", out[0].Support.Reason, "rate() not supported")
	}
}

func TestAnnotateSupport_FirstFailureWins(t *testing.T) {
	// Two failing exprs — first failure's reason wins.
	panels := []Panel{panelWith("gauge", "bad_a", "bad_b")}
	caps := &fakeCaps{
		unsupported: map[string]string{
			"bad_a": "reason-a",
			"bad_b": "reason-b",
		},
	}
	out := annotateSupport(panels, caps)

	if out[0].Support.Reason != "reason-a" {
		t.Errorf("Reason = %q, want %q", out[0].Support.Reason, "reason-a")
	}
}

func TestAnnotateSupport_EmptyExprSkipped(t *testing.T) {
	// A target with no expr must not be evaluated via IsSupported.
	// Panel still comes out "supported" if no other targets fail.
	panels := []Panel{panelWith("stat", "")}
	// Caps that would fail if called — but should not be called for empty expr.
	caps := &fakeCaps{
		unsupported: map[string]string{"": "should not be called"},
	}
	out := annotateSupport(panels, caps)

	if out[0].Support.Status != "supported" {
		t.Errorf("Status = %q, want %q", out[0].Support.Status, "supported")
	}
}

func TestAnnotateSupport_DoesNotMutateInput(t *testing.T) {
	panels := []Panel{panelWith("timeseries", "owl_runtime_goroutines")}
	caps := &fakeCaps{}
	_ = annotateSupport(panels, caps)

	// Original slice must have zero-value Support.
	if panels[0].Support.Status != "" {
		t.Error("annotateSupport must not mutate the input slice")
	}
}

func TestAnnotateSupport_MultiplePanels(t *testing.T) {
	panels := []Panel{
		panelWith("timeseries", "good_metric"),
		panelWith("unknown-viz", "whatever"),
		panelWith("stat", "also_good"),
	}
	caps := &fakeCaps{}
	out := annotateSupport(panels, caps)

	if out[0].Support.Status != "supported" {
		t.Errorf("[0] Status = %q, want supported", out[0].Support.Status)
	}
	if out[1].Support.Status != "unsupported" {
		t.Errorf("[1] Status = %q, want unsupported", out[1].Support.Status)
	}
	if out[2].Support.Status != "supported" {
		t.Errorf("[2] Status = %q, want supported", out[2].Support.Status)
	}
}
