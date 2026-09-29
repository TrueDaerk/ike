package jqplay

import "testing"

// stages_test.go covers the pipeline stepping's engine half (#2785): where a
// program's top-level stages are, and what it is when cut after one.

// stageTexts renders the stages as their text, for readable assertions.
func stageTexts(program string) []string {
	r := []rune(program)
	var out []string
	for _, st := range Stages(program) {
		out = append(out, string(r[st.Start:st.End]))
	}
	return out
}

// TestStagesSplitTopLevelPipes: the stages are the text between top-level
// pipes, the pipes and their blanks excluded.
func TestStagesSplitTopLevelPipes(t *testing.T) {
	got := stageTexts(`.items | map(.x) | add`)
	want := []string{`.items`, `map(.x)`, `add`}
	if len(got) != len(want) {
		t.Fatalf("stages = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stage %d = %q, want %q", i+1, got[i], want[i])
		}
	}
}

// TestStagesIgnoreNestedPipes: a `|` inside a string, a comment, brackets,
// `select(...)`, an `if … end` or a `def …;` body — and `||` or `|=` — is not
// a boundary of the outer pipeline.
func TestStagesIgnoreNestedPipes(t *testing.T) {
	for program, want := range map[string]int{
		`.a | select(.b | . > 1) | .c`:               3,
		`[.[] | .x] | length`:                        2,
		`{a: (.b | .c)} | .a`:                        2,
		`.a == "x|y" | not`:                          2,
		`.a # x | y`:                                 1,
		`.a || .b`:                                   1,
		`.a |= 1 | .a`:                               2,
		`if . then .a | .b else .c end | length`:     2,
		`def f: .a | .b; f | length`:                 2,
		`def f(g): g | .x; f(.a; .b) | .y`:           2,
		`{if: 1} | .if`:                              2,
		`reduce .[] as $x (0; . + $x) | . * 2`:       2,
		``:                                           1,
		`.a | ((.b | .c) | .d) | .e | ([.f] | .[0])`: 4,
	} {
		if got := len(Stages(program)); got != want {
			t.Errorf("%q: %d stages %q, want %d", program, got, stageTexts(program), want)
		}
	}
}

// TestStageAt: the caret belongs to the stage it is in or right behind; on a
// pipe or the blank after it, to the stage that follows.
func TestStageAt(t *testing.T) {
	program := `.a | .b | .c` // stages [0,2) [5,7) [10,12)
	stages := Stages(program)
	for pos, want := range map[int]int{0: 1, 2: 1, 3: 2, 4: 2, 7: 2, 8: 3, 12: 3, 99: 3} {
		if got := StageAt(stages, pos); got != want {
			t.Errorf("StageAt(%d) = %d, want %d", pos, got, want)
		}
	}
}

// TestStageProgramTruncates is the issue's acceptance case: for
// `.items | map(.x) | add` stage 1 shows the items array, stage 2 the mapped
// array, stage 3 the sum.
func TestStageProgramTruncates(t *testing.T) {
	const doc = `{"items":[{"x":1},{"x":2},{"x":3}]}`
	const program = `.items | map(.x) | add`
	for k, want := range map[int]string{
		1: `[{"x":1},{"x":2},{"x":3}]`,
		2: `[1,2,3]`,
		3: `6`,
	} {
		prog := StageProgram(program, k)
		res := Evaluate(prog, doc)
		if res.Err != "" {
			t.Fatalf("stage %d (%q): %s", k, prog, res.Err)
		}
		if len(res.Outputs) != 1 || compact(res.Outputs[0]) != want {
			t.Errorf("stage %d (%q) = %v, want %s", k, prog, res.Outputs, want)
		}
	}
	if got := StageProgram(program, 3); got != program {
		t.Errorf("the last stage is the whole program, got %q", got)
	}
	if got := StageProgram(program, 1); got != `.items` {
		t.Errorf("stage 1 = %q, want the cut text", got)
	}
}

// TestStageProgramCompletesABinding: a cut ending on `as $x` runs as the
// binding passing its input on, not as a syntax error.
func TestStageProgramCompletesABinding(t *testing.T) {
	prog := StageProgram(`.a as $x | .b | $x`, 1)
	if prog != `.a as $x | .` {
		t.Fatalf("cut = %q", prog)
	}
	res := Evaluate(prog, `{"a":1,"b":2}`)
	if res.Err != "" || len(res.Outputs) != 1 || compact(res.Outputs[0]) != `{"a":1,"b":2}` {
		t.Errorf("binding stage = %v (%s), want the input", res.Outputs, res.Err)
	}
	// An ordinary error is left alone: the user's own text is what fails.
	if got := StageProgram(`.a + | .b`, 1); got != `.a +` {
		t.Errorf("a broken stage must not be patched, got %q", got)
	}
}
