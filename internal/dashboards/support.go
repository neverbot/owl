package dashboards

import "fmt"

// annotateSupport stamps each panel with a PanelSupport status by consulting
// the provided Capabilities. It returns a new slice (the input is not mutated).
func annotateSupport(panels []Panel, caps Capabilities) []Panel {
	out := make([]Panel, len(panels))
	copy(out, panels)

	for i := range out {
		p := &out[i]
		if !supportedPanelTypes[p.Type] {
			p.Support = PanelSupport{
				Status: "unsupported",
				Reason: fmt.Sprintf("panel type %q not supported", p.Type),
			}
			continue
		}

		// Panel type is supported — check each target's expression.
		failed := false
		for _, tgt := range p.Targets {
			if tgt.Expr == "" {
				continue
			}
			ok, reason := caps.IsSupported(tgt.Expr)
			if !ok {
				p.Support = PanelSupport{
					Status: "unsupported",
					Reason: reason,
				}
				failed = true
				break
			}
		}
		if !failed {
			p.Support = PanelSupport{Status: "supported"}
		}
	}

	return out
}
