package langpython

import (
	"strings"
	"testing"

	"ike/internal/lang"
)

const pytestSample = `============================= test session starts ==============================
platform darwin -- Python 3.12.0, pytest-8.0.0, pluggy-1.4.0
rootdir: /tmp/proj
collected 4 items

test_sample.py::test_ok PASSED                                           [ 25%]
test_sample.py::test_bad FAILED                                          [ 50%]
test_sample.py::TestMath::test_add[1-2] PASSED                           [ 75%]
test_sample.py::test_skipped SKIPPED (no reason)                         [100%]

=================================== FAILURES ===================================
___________________________________ test_bad ___________________________________
test_sample.py:9: in test_bad
    assert add(1, 1) == 3
E   assert 2 == 3
=========================== short test summary info ============================
FAILED test_sample.py::test_bad - assert 2 == 3
========================= 1 failed, 2 passed, 1 skipped in 0.03s ===============
`

func TestParsePytestStatuses(t *testing.T) {
	res := parsePytest(pytestSample)
	if len(res) != 4 {
		t.Fatalf("results = %+v, want 4", res)
	}
	if res[0].Name != "test_ok" || res[0].Status != lang.TestPass || res[0].Group != "test_sample.py" {
		t.Fatalf("pass result = %+v", res[0])
	}
	if res[1].Name != "test_bad" || res[1].Status != lang.TestFail || res[1].RerunID != "test_bad" {
		t.Fatalf("fail result = %+v", res[1])
	}
	if res[1].File != "test_sample.py" || res[1].Line != 9 {
		t.Fatalf("failure location = %q:%d, want test_sample.py:9", res[1].File, res[1].Line)
	}
	if !strings.Contains(res[1].Output, "assert 2 == 3") {
		t.Fatalf("failure output = %q", res[1].Output)
	}
	if res[2].Name != "TestMath/test_add[1-2]" || res[2].Status != lang.TestPass {
		t.Fatalf("class result = %+v", res[2])
	}
	if res[2].RerunID != "test_add" {
		t.Fatalf("class rerun id = %q, want test_add (bare -k name)", res[2].RerunID)
	}
	if res[3].Status != lang.TestSkip {
		t.Fatalf("skip result = %+v", res[3])
	}
	// The short-summary FAILED line must not have become a fifth result.
	for _, r := range res {
		if strings.HasPrefix(r.Group, "FAILED") {
			t.Fatalf("summary line parsed as a result: %+v", r)
		}
	}
}

func TestParsePytestClassFailureBlock(t *testing.T) {
	out := `test_x.py::TestC::test_y FAILED [100%]

=================================== FAILURES ===================================
_______________________________ TestC.test_y _______________________________
test_x.py:21: in test_y
E   ValueError: nope
============================== 1 failed in 0.01s ===============================
`
	res := parsePytest(out)
	if len(res) != 1 {
		t.Fatalf("results = %+v", res)
	}
	if res[0].File != "test_x.py" || res[0].Line != 21 {
		t.Fatalf("location = %q:%d", res[0].File, res[0].Line)
	}
	if !strings.Contains(res[0].Output, "ValueError") {
		t.Fatalf("output = %q", res[0].Output)
	}
	if res[0].Name != "TestC/test_y" || res[0].RerunID != "test_y" {
		t.Fatalf("result = %+v", res[0])
	}
}

func TestParsePytestGarbage(t *testing.T) {
	if res := parsePytest("no tests here\n"); res != nil {
		t.Fatalf("garbage must parse to nil, got %+v", res)
	}
}
