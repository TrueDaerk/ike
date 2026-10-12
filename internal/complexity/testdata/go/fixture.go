// Package fixture is the Go golden fixture of the complexity engine (#2915).
// Every function exercises one construct family; expected values live in
// fixture.golden.json and were cross-checked against gocyclo, gocognit and
// lizard (see golden_test.go for the versions and the known differences).
package fixture

import "fmt"

// Server is the receiver of the method units.
type Server struct {
	name string
}

// elseIfChain: an if / else-if / else-if / else ladder with a nested if in
// the else branch. CCN 1 + 4 = 5; cognitive if(1) + else-if(1) + else-if(1)
// + else(1) + nested if at level 1 (2) = 6.
func elseIfChain(a int) string {
	if a == 1 {
		return "one"
	} else if a == 2 {
		return "two"
	} else if a == 3 {
		return "three"
	} else {
		/* comment-only lines, line or block,
		   do not count toward NLOC */
		if a < 0 {
			return "negative"
		}
	}
	return "many"
}

// switchArms: the switch counts once for cognitive, every non-default arm
// once for CCN; the arms' bodies — the default included — sit one level in.
func switchArms(a int) int {
	switch a {
	case 1:
		if a > 0 {
			return 1
		}
	case 2, 3:
		return 2
	default:
		if a < 0 {
			return 3
		}
	}
	return 0
}

// typeSwitchAndSelect: a type switch and a select are selectors too.
func typeSwitchAndSelect(v any, ch <-chan int) int {
	switch x := v.(type) {
	case int:
		return x
	case string:
		return len(x)
	}
	select {
	case n := <-ch:
		return n
	default:
		return -1
	}
}

// nestedLoops: a range loop, a three-clause loop and a conditional loop,
// nested three deep with a labelled continue and a plain break.
func nestedLoops(xs []int) int {
	n := 0
outer:
	for i := range xs {
		for j := 0; j < i; j++ {
			for n < 100 {
				if j == 2 {
					continue outer
				}
				if j == 3 {
					break
				}
				n++
			}
		}
	}
	return n
}

// (Server) Handle: a method, named Server.Handle, with a deferred closure
// whose branch is the closure's own CCN but the method's cognitive at level 1.
func (s *Server) Handle(req string, retries int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	if req == "" {
		return fmt.Errorf("empty")
	}
	return nil
}

// booleanSequences: `a && b && c` is CCN +2, cognitive +1; `a && b || c` is
// CCN +2, cognitive +2; parentheses are flattened (`a && (b || c) && d` is
// three runs).
func booleanSequences(a, b, c, d bool) int {
	if a && b && c {
		return 1
	}
	if a && b || c {
		return 2
	}
	if a && (b || c) && d {
		return 3
	}
	if a || b && c {
		return 4
	}
	return 0
}

// nestedClosures: a closure inside a closure; each is its own unit, and
// their branches count toward the parent's cognitive at levels 1 and 2.
func nestedClosures(xs []int) func() int {
	return func() int {
		sum := 0
		each := func(x int) {
			if x > 0 {
				sum += x
			}
		}
		for _, x := range xs {
			each(x)
		}
		return sum
	}
}

// factorial: direct recursion, +1 cognitive per recursive call.
func factorial(n int) int {
	if n <= 1 {
		return 1
	}
	return n * factorial(n-1)
}

// labelledJumps: goto and a labelled break are jumps; a plain break is free.
func labelledJumps(xs []int) int {
	i := 0
loop:
	for _, x := range xs {
		if x < 0 {
			break loop
		}
		i++
	}
	if i > 10 {
		goto done
	}
	for {
		break
	}
done:
	return i
}

// plain has no branches at all.
func plain() {}
