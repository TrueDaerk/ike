package testdata

// templates.go ships the built-in templates (#2392): format-free DSL bodies
// covering the common sample-file shapes, and — deliberately — the DSL's own
// feature set, so picking one doubles as a working example of references,
// template strings and weighted alternatives. Every body is parsed by a test;
// a built-in that fails to load is a build bug, not a runtime condition.

var builtinTemplates = []Template{
	{Name: "Person", BuiltIn: true, DSL: `id         = id()
first_name = first_name()
last_name  = last_name()
full_name  = "{first_name} {last_name}"
email      = email()
phone      = phone()
birthday   = date(1960-01-01..2008-01-01)
`},
	{Name: "Address", BuiltIn: true, DSL: `id      = id()
street  = street()
city    = city()
country = country()
company = company()
`},
	{Name: "Order", BuiltIn: true, DSL: `id       = id()
customer = full_name()
item     = from_list(widget, gadget, gizmo, doohickey)
quantity = int(1..12)
price    = float(0.5..500)
ordered  = date(2024-01-01..2026-01-01)
status   = weighted(70: "shipped", 20: "pending", 10: "cancelled")
`},
	{Name: "URL / Web", BuiltIn: true, DSL: `id     = id()
domain = domain()
host   = hostname({domain})
url    = "https://{host}/api/{id}"
state  = weighted(60: "active", 20: "inactive", 20: "banned")
email  = weighted(70: email({domain}), 30: "")
`},
	{Name: "Server log", BuiltIn: true, DSL: `ip     = ipv4()
method = weighted(70: "GET", 20: "POST", 5: "PUT", 5: "DELETE")
path   = "/api/v1/users/{user}"
user   = int(1..5000)
status = weighted(85: "200", 5: "301", 7: "404", 3: "500")
bytes  = int(120..250000)
agent  = user_agent()
`},
}

// BuiltinTemplates returns the shipped templates in their curated order.
func BuiltinTemplates() []Template {
	return append([]Template(nil), builtinTemplates...)
}
