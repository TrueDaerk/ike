package config

// write.go exposes the typed setter seam. This roadmap is read/merge only, so
// the setters mutate an in-memory *Config and define the bounded semantics;
// persisting the result back to disk is the owning roadmap's job (Roadmap 0090
// for project.history). Keeping the mutation rules here means the write UX layer
// never re-implements the bounding logic.

// PushHistory records root as the most-recent project: it is moved (or added) to
// the front, de-duplicated, and the list is trimmed to MaxHistory. It returns the
// modified config for chaining.
func (c *Config) PushHistory(root string) *Config {
	if root == "" {
		return c
	}
	out := []string{root}
	for _, h := range c.Project.History {
		if h != root {
			out = append(out, h)
		}
	}
	if n := c.Project.MaxHistory; n >= 0 && len(out) > n {
		out = out[:n]
	}
	c.Project.History = out
	return c
}
