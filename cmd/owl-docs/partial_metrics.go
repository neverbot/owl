package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/neverbot/owl/internal/web"
)

func init() {
	registerPartial("metrics-table", metricsTablePartial)
}

// metricsTablePartial emits one Markdown table per metric family using
// [web.Registry] as the source of truth. Families and the metrics
// within each family are sorted alphabetically for deterministic
// output.
func metricsTablePartial(args map[string]string) (string, error) {
	byFamily := map[string][]web.MetricDescriptor{}
	for _, d := range web.Registry() {
		fam := d.Family
		if fam == "" {
			fam = "misc"
		}
		byFamily[fam] = append(byFamily[fam], d)
	}
	families := make([]string, 0, len(byFamily))
	for f := range byFamily {
		families = append(families, f)
	}
	sort.Strings(families)

	var b strings.Builder
	for _, fam := range families {
		fmt.Fprintf(&b, "\n#### `%s` family\n\n", fam)
		fmt.Fprintf(&b, "| Metric | Type | Description |\n|---|---|---|\n")
		ds := byFamily[fam]
		sort.Slice(ds, func(i, j int) bool { return ds[i].Name < ds[j].Name })
		for _, d := range ds {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", d.Name, d.Type, escapePipe(d.Help))
		}
	}
	return b.String(), nil
}
