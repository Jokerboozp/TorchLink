package capacity

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestPageBuiltPlansAreValid runs the page's plan builder (iot_front/src/ops/
// capacity.js) for every preset and option combination and validates the
// result with the Go rules, so the form can never produce a plan the service
// rejects.
func TestPageBuiltPlansAreValid(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	script := `
const m = await import(process.argv[1]);
const out = [];
for (const preset of ['quick', 'capacity', 'soak']) {
  for (let bits = 0; bits < 64; bits++) {
    const f = m.defaultForm(preset);
    ['mqtt', 'alarms', 'queries', 'realtime', 'exports', 'ai'].forEach((k, i) => { f[k] = Boolean(bits & (1 << i)); });
    out.push({ preset, bits, problems: m.formProblems(f), plan: m.buildPlan(f) });
  }
  const edge = m.defaultForm(preset);
  edge.devices = 3; edge.startRate = 5; edge.maxRate = m.rateCeiling(edge); edge.mqtt = true;
  out.push({ preset, bits: -1, problems: m.formProblems(edge), plan: m.buildPlan(edge) });
}
console.log(JSON.stringify(out));`
	cmd := exec.Command(node, "--input-type=module", "-e", script, filepath.Join(root, "iot_front", "src", "ops", "capacity.js"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Preset   string
		Bits     int
		Problems []string
		Plan     string
	}
	if err = json.Unmarshal(out, &cases); err != nil || len(cases) != 3*65 {
		t.Fatal(err, len(cases))
	}
	for _, c := range cases {
		if len(c.Problems) > 0 {
			t.Fatalf("%s/%d default form reports problems: %v", c.Preset, c.Bits, c.Problems)
		}
		p, err := ParsePlan([]byte(c.Plan))
		if err != nil {
			t.Fatalf("%s/%d: %v\n%s", c.Preset, c.Bits, err, c.Plan)
		}
		p.Fixtures.Tenant = "tenant_001" // set by the platform
		if err = p.Validate(); err != nil {
			t.Fatalf("%s/%d: %v\n%s", c.Preset, c.Bits, err, c.Plan)
		}
		if p.Fixtures.Product != AutoProductID || (p.Fixtures.AlarmFraction > 0) != (p.Fixtures.AlarmRuleID == AutoRuleID) {
			t.Fatalf("%s/%d fixtures %+v", c.Preset, c.Bits, p.Fixtures)
		}
	}
}
