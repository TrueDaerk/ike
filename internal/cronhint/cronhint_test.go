package cronhint

import "testing"

// TestDescribe (#1624) pins the English rendering across the expression
// shapes people actually write: steps, lists, ranges, names, macros and the
// optional seconds field.
func TestDescribe(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"*/5 * * * *", "every 5 min"},
		{"* * * * *", "every min"},
		{"0-59 * * * *", "every min"}, // an explicit full range reads as "*"
		{"0 3 * * 1", "Mon 03:00"},
		{"0 3 * * *", "daily 03:00"},
		{"30 4 * * 1-5", "Mon-Fri 04:30"},
		{"0 0 * * MON-FRI", "Mon-Fri 00:00"},
		{"0 0 * * 7", "Sun 00:00"}, // Sunday as 7 normalises to 0
		{"0 */2 * * *", "every 2 h"},
		{"15 */6 * * *", "every 6 h :15"},
		{"0 9,17 * * *", "daily 09:00,17:00"},
		{"0 * * * *", "hourly :00"},
		{"5,35 * * * *", "hourly :05,:35"},
		{"* 3 * * *", "every min 03h"},
		{"*/5 9-17 * * 1-5", "Mon-Fri every 5 min 09-17h"},
		{"0 0 1 * *", "day 1 00:00"},
		{"0 0 1,15 * *", "day 1,15 00:00"},
		{"0 0 */2 * *", "every 2 days 00:00"},
		{"5 4 * JAN *", "Jan 04:05"},
		{"0 0 1 1 *", "Jan day 1 00:00"},
		{"30 3 15 * 2", "day 15 or Tue 03:30"}, // cron ORs dom and dow
		{"10 0 * * ?", "daily 00:10"},          // Quartz "no specific value"
		{"*/30 * * * * *", "every 30 sec"},     // six fields: seconds first
		{"0 0 0 * * *", "daily 00:00"},
		{"30 15 10 * * *", "daily 10:15:30"},
		{"@daily", "daily 00:00"},
		{"@weekly", "Sun 00:00"},
		{"@reboot", "at boot"},
		{"  0 3 * * 1  ", "Mon 03:00"}, // surrounding whitespace is trimmed
	}
	for _, c := range cases {
		got, ok := Describe(c.expr)
		if !ok {
			t.Errorf("Describe(%q): not recognised", c.expr)
			continue
		}
		if got != c.want {
			t.Errorf("Describe(%q) = %q, want %q", c.expr, got, c.want)
		}
	}
}

// TestDescribeRejects (#1624): expressions this package cannot read produce no
// hint at all — a wrong schedule is worse than none.
func TestDescribeRejects(t *testing.T) {
	for _, expr := range []string{
		"",
		"* * *",                  // too few fields
		"0 0 * * * * 2030",       // Quartz' seventh (year) field
		"0 0 L * *",              // last-day-of-month extension
		"0 0 * * 5#3",            // nth-weekday extension
		"0 0 * * 1W",             // nearest-weekday extension
		"0 0 * * FUN",            // not a day name
		"60 0 * * *",             // minute out of range
		"0 24 * * *",             // hour out of range
		"0 0 32 * *",             // day out of range
		"*/0 * * * *",            // zero step
		"5-1 * * * *",            // inverted range
		"@quarterly",             // not a crontab macro
		"echo hello world now !", // plain prose
	} {
		if got, ok := Describe(expr); ok {
			t.Errorf("Describe(%q) = %q, want no hint", expr, got)
		}
	}
}

// TestDescribeTruncates (#1624): a hint stays compact — a schedule with many
// clock times shows the first few and a count.
func TestDescribeTruncates(t *testing.T) {
	got, ok := Describe("0,15,30,45 1,2 * * *")
	if !ok {
		t.Fatal("Describe: not recognised")
	}
	if want := "daily 01:00,01:15,01:30,+5"; got != want {
		t.Errorf("Describe = %q, want %q", got, want)
	}
}
