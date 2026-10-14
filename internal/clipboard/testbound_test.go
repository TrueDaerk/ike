package clipboard

import "time"

// The tests drive shell-script fakes, where the 3 s production bound on a
// clipboard subprocess (#2163) only measures how loaded the machine is: the
// full suite under package-level parallelism has seen /bin/sh take longer
// than that and the test die with "signal: killed" (#2997). The fakes are
// never stuck, so the bound is widened for the whole test binary; the
// deadline test sets its own short bound and restores this one.
func init() { cmdTimeout = 60 * time.Second }
