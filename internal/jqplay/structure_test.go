package jqplay

import "testing"

func TestStructureRainbowAndPipes(t *testing.T) {
	p := `.a | map(select(.x > 1)) | {n: .name}`
	m := Structure(p, Tokens(p), true)
	r := []rune(p)
	depths := map[int]bool{}
	pipes := 0
	for i, mk := range m {
		if mk.Unmatched {
			t.Fatalf("unexpected unmatched at %d", i)
		}
		if mk.Bracket {
			depths[mk.Depth] = true
		}
		if mk.Pipe {
			pipes++
			if r[i] != '|' {
				t.Fatalf("pipe mark on %q", r[i])
			}
		}
	}
	if len(depths) != 2 || pipes != 2 {
		t.Fatalf("depths %v pipes %d", depths, pipes)
	}
	// A pipe inside brackets is not a stage break; three depths nest here.
	p = `map(select(.x | tostring | [.]))`
	m = Structure(p, Tokens(p), true)
	depths = map[int]bool{}
	for _, mk := range m {
		if mk.Pipe {
			t.Fatal("nested pipe emphasised")
		}
		depths[mk.Depth] = true
	}
	if len(depths) != 3 {
		t.Fatalf("depths %v", depths)
	}
}

func TestStructureStringsAndUnbalanced(t *testing.T) {
	p := `"a|(b" | .x |= 1`
	m := Structure(p, Tokens(p), true)
	if len(m) != 1 || !m[7].Pipe {
		t.Fatalf("marks %v", m)
	}
	p = `.a | (] | {`
	m = Structure(p, Tokens(p), true)
	if !m[5].Unmatched || !m[6].Unmatched || !m[10].Unmatched || !m[3].Pipe || m[8].Pipe {
		t.Fatalf("marks %v", m)
	}
	if got := Structure(`a | b`, Tokens(`a | b`), false); len(got) != 0 {
		t.Fatalf("xmq pipes %v", got)
	}
}
