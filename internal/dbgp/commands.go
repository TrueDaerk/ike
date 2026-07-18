package dbgp

import (
	"encoding/base64"
	"fmt"
)

// commands.go wraps the raw Call vocabulary in typed helpers — exactly the
// commands the DAP bridge (#699/#700) needs.

// FeatureSet sets an engine feature (max_depth, max_children, …).
func (c *Conn) FeatureSet(name, value string) error {
	_, err := c.Call("feature_set", joinFlags("-n", name, "-v", value), "")
	return err
}

// BreakpointSet sets a line breakpoint and returns the engine's breakpoint id.
func (c *Conn) BreakpointSet(fileURI string, line int) (string, error) {
	resp, err := c.Call("breakpoint_set",
		joinFlags("-t", "line", "-f", fileURI, "-n", fmt.Sprint(line)), "")
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// BreakpointRemove removes a breakpoint by engine id.
func (c *Conn) BreakpointRemove(id string) error {
	_, err := c.Call("breakpoint_remove", joinFlags("-d", id), "")
	return err
}

// Run resumes execution until the next break or the end of the run. Blocks
// until then — issue on a goroutine when the caller must not wait.
func (c *Conn) Run() (*Response, error) { return c.Call("run", nil, "") }

// StepInto steps to the next statement, entering calls.
func (c *Conn) StepInto() (*Response, error) { return c.Call("step_into", nil, "") }

// StepOver steps to the next statement in the same frame.
func (c *Conn) StepOver() (*Response, error) { return c.Call("step_over", nil, "") }

// StepOut runs until the current frame returns.
func (c *Conn) StepOut() (*Response, error) { return c.Call("step_out", nil, "") }

// Stop ends the debug session; the engine terminates the script.
func (c *Conn) Stop() (*Response, error) { return c.Call("stop", nil, "") }

// StackGet returns the current stack, innermost frame first (level 0).
func (c *Conn) StackGet() ([]StackEntry, error) {
	resp, err := c.Call("stack_get", nil, "")
	if err != nil {
		return nil, err
	}
	return resp.Stack, nil
}

// ContextNames lists the variable contexts of frame depth (0 = innermost).
func (c *Conn) ContextNames(depth int) ([]ContextName, error) {
	resp, err := c.Call("context_names", joinFlags("-d", fmt.Sprint(depth)), "")
	if err != nil {
		return nil, err
	}
	return resp.Contexts, nil
}

// ContextGet returns the variables of context contextID at frame depth.
func (c *Conn) ContextGet(depth, contextID int) ([]Property, error) {
	resp, err := c.Call("context_get",
		joinFlags("-d", fmt.Sprint(depth), "-c", fmt.Sprint(contextID)), "")
	if err != nil {
		return nil, err
	}
	return resp.Properties, nil
}

// PropertyGet fetches one property by fullname at frame depth, page page —
// the way to expand structured values beyond the engine's max_children.
func (c *Conn) PropertyGet(fullname string, depth, page int) (*Property, error) {
	resp, err := c.Call("property_get",
		joinFlags("-n", fullname, "-d", fmt.Sprint(depth), "-p", fmt.Sprint(page)), "")
	if err != nil {
		return nil, err
	}
	if len(resp.Properties) == 0 {
		return nil, fmt.Errorf("dbgp: property %q not found", fullname)
	}
	return &resp.Properties[0], nil
}

// PropertySet assigns value to the property fullname at frame depth. The
// value travels base64-encoded as the command's data block.
func (c *Conn) PropertySet(fullname string, depth int, value string) error {
	_, err := c.Call("property_set",
		joinFlags("-n", fullname, "-d", fmt.Sprint(depth)),
		base64.StdEncoding.EncodeToString([]byte(value)))
	return err
}

// Eval evaluates a PHP expression in the current frame and returns its
// result property.
func (c *Conn) Eval(expr string) (*Property, error) {
	resp, err := c.Call("eval", nil, base64.StdEncoding.EncodeToString([]byte(expr)))
	if err != nil {
		return nil, err
	}
	if len(resp.Properties) == 0 {
		return nil, fmt.Errorf("dbgp: eval returned no result")
	}
	return &resp.Properties[0], nil
}
