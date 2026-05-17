package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/neverbot/owl/internal/query"
)

func init() {
	registerPartial("promql-capabilities", promqlCapabilitiesPartial)
}

// promqlCapabilitiesPartial emits a grouped, sorted Markdown list of
// every PromQL construct [query.Engine] supports, reading the public
// fields of [query.Capabilities] directly (Functions, Aggrs, Matchers,
// Operators).
func promqlCapabilitiesPartial(args map[string]string) (string, error) {
	c := (&query.Engine{}).Capabilities()
	var b strings.Builder
	emitCapabilityList(&b, "Functions", c.Functions)
	emitCapabilityList(&b, "Aggregations", c.Aggrs)
	emitCapabilityList(&b, "Label matchers", c.Matchers)
	emitCapabilityList(&b, "Binary operators", c.Operators)
	return b.String(), nil
}

// emitCapabilityList writes one labelled, sorted, backtick-quoted line
// of items. A nil or empty slice produces nothing.
func emitCapabilityList(b *strings.Builder, header string, items []string) {
	if len(items) == 0 {
		return
	}
	cp := append([]string(nil), items...)
	sort.Strings(cp)
	fmt.Fprintf(b, "\n**%s:** ", header)
	for i, it := range cp {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "`%s`", it)
	}
	b.WriteString("\n")
}
