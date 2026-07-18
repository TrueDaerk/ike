package bridge

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"ike/internal/dbgp"
)

// variables.go maps the DAP scopes/variables/setVariable vocabulary onto
// DBGp contexts and properties (0360, #700). References are allocated per
// stopped state and dropped on resume — DAP clients only use them while
// paused, and the manager refetches on every stop.

// maxVariableChildren caps how many children of one structured value are
// fetched across property_get pages; the rest is indicated by the parent's
// count in its rendered value.
const maxVariableChildren = 1000

// varRef is one live variablesReference: either a whole context (Locals,
// Superglobals) of a frame, or one structured property expanded by
// fullname. names maps the served child rows to their DBGp fullnames so
// setVariable can address them.
type varRef struct {
	depth    int
	context  int
	fullname string // empty for context refs
	names    map[string]string
}

// varTable owns the reference numbering of one paused state.
type varTable struct {
	nextRef int
	refs    map[int]*varRef
}

func newVarTable() *varTable {
	return &varTable{refs: map[int]*varRef{}}
}

func (t *varTable) alloc(r *varRef) int {
	t.nextRef++
	t.refs[t.nextRef] = r
	return t.nextRef
}

// resetVars drops all references (called on every resume).
func (b *bridge) resetVars() {
	b.mu.Lock()
	b.vars = nil
	b.mu.Unlock()
}

// varsTable returns the current table, creating it lazily on first use
// after a stop.
func (b *bridge) varsTable() *varTable {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.vars == nil {
		b.vars = newVarTable()
	}
	return b.vars
}

// lookupRef resolves a variablesReference; nil when stale.
func (b *bridge) lookupRef(ref int) *varRef {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.vars == nil {
		return nil
	}
	return b.vars.refs[ref]
}

// handleScopes maps a frame's contexts to DAP scopes. Frame ids encode the
// DBGp depth as id-1 (see handleStackTrace).
func (b *bridge) handleScopes(req envelope) {
	var args struct {
		FrameID int `json:"frameId"`
	}
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		b.fail(req, "invalid scopes arguments")
		return
	}
	dc := b.conn()
	if dc == nil {
		b.fail(req, "no debug session")
		return
	}
	depth := args.FrameID - 1
	ctxs, err := dc.ContextNames(depth)
	if err != nil {
		b.fail(req, err.Error())
		return
	}
	table := b.varsTable()
	scopes := make([]map[string]any, 0, len(ctxs))
	b.mu.Lock()
	for _, c := range ctxs {
		ref := table.alloc(&varRef{depth: depth, context: c.ID, names: map[string]string{}})
		scopes = append(scopes, map[string]any{
			"name":               c.Name,
			"variablesReference": ref,
			// Superglobals & co. are bulky; everything but the first context
			// is marked expensive so panels can defer expanding it.
			"expensive": c.ID != 0,
		})
	}
	b.mu.Unlock()
	b.respond(req, map[string]any{"scopes": scopes})
}

// handleVariables expands one reference: context_get for scope refs,
// paged property_get for structured values.
func (b *bridge) handleVariables(req envelope) {
	var args struct {
		VariablesReference int `json:"variablesReference"`
	}
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		b.fail(req, "invalid variables arguments")
		return
	}
	dc := b.conn()
	if dc == nil {
		b.fail(req, "no debug session")
		return
	}
	vr := b.lookupRef(args.VariablesReference)
	if vr == nil {
		// Stale reference after a resume: empty, not an error.
		b.respond(req, map[string]any{"variables": []any{}})
		return
	}
	var props []dbgp.Property
	var err error
	if vr.fullname == "" {
		props, err = dc.ContextGet(vr.depth, vr.context)
	} else {
		props, err = b.fetchChildren(dc, vr)
	}
	if err != nil {
		b.fail(req, err.Error())
		return
	}
	table := b.varsTable()
	out := make([]map[string]any, 0, len(props))
	b.mu.Lock()
	for _, p := range props {
		name := p.Name
		if name == "" {
			name = p.Fullname
		}
		row := map[string]any{
			"name":  name,
			"value": renderValue(p),
			"type":  renderType(p),
		}
		if p.HasChildren == 1 && p.Fullname != "" {
			row["variablesReference"] = table.alloc(&varRef{depth: vr.depth, context: vr.context, fullname: p.Fullname, names: map[string]string{}})
		} else {
			row["variablesReference"] = 0
		}
		if p.Fullname != "" {
			vr.names[name] = p.Fullname
		}
		out = append(out, row)
	}
	b.mu.Unlock()
	b.respond(req, map[string]any{"variables": out})
}

// fetchChildren pulls a structured property's children across pages until
// all (or maxVariableChildren) are loaded.
func (b *bridge) fetchChildren(dc *dbgp.Conn, vr *varRef) ([]dbgp.Property, error) {
	first, err := dc.PropertyGet(vr.fullname, vr.depth, 0)
	if err != nil {
		return nil, err
	}
	children := first.Children
	pageSize := first.PageSize
	for page := 1; pageSize > 0 &&
		len(children) < first.NumChildren &&
		len(children) < maxVariableChildren &&
		page*pageSize < first.NumChildren; page++ {
		next, err := dc.PropertyGet(vr.fullname, vr.depth, page)
		if err != nil {
			break // partial expansion beats an error row
		}
		if len(next.Children) == 0 {
			break
		}
		children = append(children, next.Children...)
	}
	return children, nil
}

// handleSetVariable assigns a new value via property_set and echoes the
// engine's resulting value back.
func (b *bridge) handleSetVariable(req envelope) {
	var args struct {
		VariablesReference int    `json:"variablesReference"`
		Name               string `json:"name"`
		Value              string `json:"value"`
	}
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		b.fail(req, "invalid setVariable arguments")
		return
	}
	dc := b.conn()
	if dc == nil {
		b.fail(req, "no debug session")
		return
	}
	vr := b.lookupRef(args.VariablesReference)
	if vr == nil {
		b.fail(req, "variable reference is stale")
		return
	}
	b.mu.Lock()
	fullname := vr.names[args.Name]
	b.mu.Unlock()
	if fullname == "" {
		b.fail(req, "unknown variable "+args.Name)
		return
	}
	if err := dc.PropertySet(fullname, vr.depth, args.Value); err != nil {
		b.fail(req, err.Error())
		return
	}
	echo, err := dc.PropertyGet(fullname, vr.depth, 0)
	if err != nil {
		// The set succeeded; echo the raw input rather than failing the edit.
		b.respond(req, map[string]any{"value": args.Value})
		return
	}
	b.respond(req, map[string]any{
		"value": renderValue(*echo),
		"type":  renderType(*echo),
	})
}

// renderValue renders one property the way the debug panel shows values:
// strings quoted, aggregates as counts/class names, scalars raw.
func renderValue(p dbgp.Property) string {
	switch p.Type {
	case "array":
		return fmt.Sprintf("array(%d)", p.NumChildren)
	case "object":
		if p.ClassName != "" {
			return p.ClassName
		}
		return "object"
	case "null":
		return "null"
	case "uninitialized":
		return "<uninitialized>"
	case "string":
		v := p.Value()
		q := strconv.Quote(v)
		if p.Size > len(v) {
			// max_data clipped the value; show that it goes on.
			q = strings.TrimSuffix(q, `"`) + `…"`
		}
		return q
	case "resource":
		return "resource(" + p.Value() + ")"
	default:
		return p.Value()
	}
}

// renderType renders the DAP type label.
func renderType(p dbgp.Property) string {
	if p.Type == "object" && p.ClassName != "" {
		return p.ClassName
	}
	if p.Type == "string" {
		return fmt.Sprintf("string(%d)", p.Size)
	}
	return p.Type
}
