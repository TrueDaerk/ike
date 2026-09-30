package agenttrace

import (
	"path/filepath"
	"time"
)

// link.go resolves trace file nodes to change-feed entries (#2838): the
// external writes IKE's watcher recorded (internal/changefeed) that the
// traced session's tool calls caused. The package stays free of the feed
// itself — the host hands in the few facts matching needs as Change values.
//
// A node and a change match on all three of
//
//   - path: the node's file is the change's file (relative node paths are
//     resolved against the session's working directory);
//   - time: the change's recorded span overlaps the tool call's window, from
//     the call being issued to its result arriving, widened by LinkSlack for
//     the watcher's debounce (a pending call is open for LinkPending);
//   - source process: the change is attributed to exactly the terminal the
//     trace follows. An unattributed change — nothing busy, or several
//     processes busy at once — never matches: the feed refused to name a
//     culprit, and the trace must not name one either.
//
// Reads never match; only a tool call that writes can have caused a change.

// Change is the part of a change-feed entry matching reads.
type Change struct {
	Path string
	// First and Last span the external events coalesced into the entry.
	First time.Time
	Last  time.Time
	// SourceKey is the session key of the one terminal the change is
	// attributed to; "" when it could not be attributed.
	SourceKey string
}

// LinkSlack widens a tool call's window on both sides: the watcher reports a
// write after its debounce, and transcript timestamps come from another
// clock than IKE's.
const LinkSlack = 5 * time.Second

// LinkPending is how long a call whose result has not arrived stays open —
// a permission prompt can hold a write back for a while.
const LinkPending = 10 * time.Minute

// Links is the resolved matching in both directions.
type Links struct {
	// ByNode maps a node key to the change path it links to.
	ByNode map[string]string
	// ByPath maps a change path to the key of the file node that caused its
	// newest write — the change feed's back-link.
	ByPath map[string]string
}

// Node returns the change path linked to the node key, "" for none.
func (l Links) Node(key string) string { return l.ByNode[key] }

// Path returns the file node key a change path links back to, "" for none.
func (l Links) Path(path string) string { return l.ByPath[path] }

// Len reports how many changes are linked.
func (l Links) Len() int { return len(l.ByPath) }

// Link matches the tree's writing file nodes against changes attributed to
// the terminal keyed follow (see the file comment); cwd resolves relative
// node paths. A tool node that touched a single file shares its file node's
// link. With no follow key nothing matches: the trace does not know which
// process is the agent.
func Link(nodes []Node, changes []Change, follow, cwd string) Links {
	out := Links{ByNode: map[string]string{}, ByPath: map[string]string{}}
	if follow == "" || len(changes) == 0 {
		return out
	}
	byPath := map[string][]Change{}
	for _, c := range changes {
		if c.SourceKey != follow || c.Path == "" {
			continue
		}
		p := filepath.Clean(c.Path)
		byPath[p] = append(byPath[p], c)
	}
	if len(byPath) == 0 {
		return out
	}
	best := map[string]*Node{} // change path -> the node the back-link names
	var visit func(ns []Node, parent *Node)
	visit = func(ns []Node, parent *Node) {
		for i := range ns {
			n := &ns[i]
			if n.Kind == NodeFile && n.Ref != nil && n.Ref.Op != OpRead {
				if c, ok := matchNode(n, byPath, cwd); ok {
					out.ByNode[n.Key] = c.Path
					if parent != nil && parent.Kind == NodeTool && parent.Ref != nil {
						out.ByNode[parent.Key] = c.Path
					}
					if prev := best[c.Path]; prev == nil || causes(n, prev, c.Last) {
						best[c.Path] = n
						out.ByPath[c.Path] = n.Key
					}
				}
			}
			visit(n.Children, n)
		}
	}
	visit(nodes, nil)
	return out
}

// causes reports whether n explains a change whose newest write landed at
// last better than prev: a call issued by then beats one issued after (which
// only matched inside the slack), the latest such call wins, and among calls
// issued after it the earliest does.
func causes(n, prev *Node, last time.Time) bool {
	nBy, prevBy := !n.At.After(last), !prev.At.After(last)
	switch {
	case nBy != prevBy:
		return nBy
	case nBy:
		return !n.At.Before(prev.At)
	}
	return n.At.Before(prev.At)
}

// matchNode returns the change n caused, if any.
func matchNode(n *Node, byPath map[string][]Change, cwd string) (Change, bool) {
	p := n.Ref.Path
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	p = filepath.Clean(p)
	cs, ok := byPath[p]
	if !ok || n.At.IsZero() {
		return Change{}, false
	}
	lo := n.At.Add(-LinkSlack)
	hi := n.At.Add(LinkPending)
	if !n.Until.IsZero() {
		hi = n.Until.Add(LinkSlack)
	}
	for _, c := range cs {
		first := c.First
		if first.IsZero() {
			first = c.Last
		}
		if !first.After(hi) && !c.Last.Before(lo) {
			return c, true
		}
	}
	return Change{}, false
}
