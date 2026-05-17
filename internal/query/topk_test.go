package query

import (
	"testing"

	"github.com/neverbot/owl/internal/storage"
)

func TestParseTopK(t *testing.T) {
	node, err := Parse("topk(3, cpu)")
	if err != nil {
		t.Fatal(err)
	}
	tk, ok := node.(*TopKNode)
	if !ok {
		t.Fatalf("expected *TopKNode, got %T", node)
	}
	if tk.Op != "topk" {
		t.Errorf("op: want topk, got %q", tk.Op)
	}
	if tk.K != 3 {
		t.Errorf("k: want 3, got %d", tk.K)
	}
	if _, ok := tk.Expr.(*SelectorNode); !ok {
		t.Errorf("expr: expected *SelectorNode, got %T", tk.Expr)
	}
}

func TestParseBottomK(t *testing.T) {
	node, err := Parse("bottomk(2, rate(reqs_total[1m]))")
	if err != nil {
		t.Fatal(err)
	}
	tk, ok := node.(*TopKNode)
	if !ok {
		t.Fatalf("expected *TopKNode, got %T", node)
	}
	if tk.Op != "bottomk" {
		t.Errorf("op: want bottomk, got %q", tk.Op)
	}
	if tk.K != 2 {
		t.Errorf("k: want 2, got %d", tk.K)
	}
	if _, ok := tk.Expr.(*RangeFuncNode); !ok {
		t.Errorf("expr: expected *RangeFuncNode, got %T", tk.Expr)
	}
}

func TestParseTopKInvalid(t *testing.T) {
	for _, expr := range []string{
		"topk(cpu)",      // missing k
		"topk(0, cpu)",   // k must be > 0
		"topk(-1, cpu)",  // negative
		"topk(1.5, cpu)", // non-integer
	} {
		if _, err := Parse(expr); err == nil {
			t.Errorf("Parse(%q): expected error, got nil", expr)
		}
	}
}

func TestEvalTopKPicksLargest(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 10}})
	q.addSeries("cpu", map[string]string{"host": "b"}, []storage.Point{{TS: 1000, Value: 30}})
	q.addSeries("cpu", map[string]string{"host": "c"}, []storage.Point{{TS: 1000, Value: 20}})

	node, err := Parse("topk(2, cpu)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Fatalf("want 2 series, got %d", len(series))
	}
	got := map[string]float64{}
	for _, s := range series {
		got[s.Labels["host"]] = s.Points[0].Value
	}
	if got["b"] != 30 || got["c"] != 20 {
		t.Errorf("topk(2) selected wrong hosts: %v", got)
	}
	if _, has := got["a"]; has {
		t.Errorf("topk(2) should not include host a (value 10): %v", got)
	}
}

func TestEvalBottomKPicksSmallest(t *testing.T) {
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 10}})
	q.addSeries("cpu", map[string]string{"host": "b"}, []storage.Point{{TS: 1000, Value: 30}})
	q.addSeries("cpu", map[string]string{"host": "c"}, []storage.Point{{TS: 1000, Value: 20}})

	node, err := Parse("bottomk(1, cpu)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("want 1 series, got %d", len(series))
	}
	if series[0].Labels["host"] != "a" {
		t.Errorf("bottomk(1) want host=a, got %v", series[0].Labels)
	}
	if series[0].Points[0].Value != 10 {
		t.Errorf("bottomk(1) value: want 10, got %v", series[0].Points[0].Value)
	}
}

func TestEvalTopKPerTimestamp(t *testing.T) {
	// Two timestamps where the leader switches. topk(1) should emit
	// host=a at t=1000 (value 50, beating 10) and host=b at t=2000
	// (value 100, beating 5).
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{
		{TS: 1000, Value: 50},
		{TS: 2000, Value: 5},
	})
	q.addSeries("cpu", map[string]string{"host": "b"}, []storage.Point{
		{TS: 1000, Value: 10},
		{TS: 2000, Value: 100},
	})

	node, err := Parse("topk(1, cpu)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	// Both host series should appear in the output, each with only
	// the timestamp at which it was the leader.
	if len(series) != 2 {
		t.Fatalf("want 2 series, got %d", len(series))
	}
	byHost := map[string][]storage.Point{}
	for _, s := range series {
		byHost[s.Labels["host"]] = s.Points
	}
	if len(byHost["a"]) != 1 || byHost["a"][0].TS != 1000 || byHost["a"][0].Value != 50 {
		t.Errorf("host a points: %+v", byHost["a"])
	}
	if len(byHost["b"]) != 1 || byHost["b"][0].TS != 2000 || byHost["b"][0].Value != 100 {
		t.Errorf("host b points: %+v", byHost["b"])
	}
}

func TestEvalTopKLargerThanInput(t *testing.T) {
	// k=10 with only 2 input series: keep both.
	q := newFakeQuerier()
	q.addSeries("cpu", map[string]string{"host": "a"}, []storage.Point{{TS: 1000, Value: 1}})
	q.addSeries("cpu", map[string]string{"host": "b"}, []storage.Point{{TS: 1000, Value: 2}})

	node, err := Parse("topk(10, cpu)")
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvaluator(q, 0, 2000)
	series, err := ev.eval(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 2 {
		t.Errorf("want 2 series, got %d", len(series))
	}
}
