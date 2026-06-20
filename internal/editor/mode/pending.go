package mode

// Pending accumulates the parts of a normal-mode command before the motion or
// text object that completes it arrives: an optional register, an optional
// count, and an optional operator. The editor's key handler feeds digits,
// `"x` register selections, and operator keys into it, then asks Resolve-style
// helpers for the effective count and clears it once a command commits.
type Pending struct {
	Operator rune // 'd' 'c' 'y' '>' '<' '=' ; 0 when none
	Count    int  // accumulated count; 0 means "unset" (treated as 1)
	Register rune // selected named register; 0 when none
	// awaitReg is set after a `"` so the next key is captured as the register.
	awaitReg bool
}

// Empty reports whether no operator, count, or register is pending.
func (p Pending) Empty() bool {
	return p.Operator == 0 && p.Count == 0 && p.Register == 0 && !p.awaitReg
}

// HasOperator reports whether an operator is waiting for its motion/text object.
func (p Pending) HasOperator() bool { return p.Operator != 0 }

// AwaitingRegister reports whether the previous key was `"`, so the next key
// names the register.
func (p Pending) AwaitingRegister() bool { return p.awaitReg }

// BeginRegister records that a `"` was typed; the next key is the register name.
func (p *Pending) BeginRegister() { p.awaitReg = true }

// SetRegister stores the register name and clears the await flag.
func (p *Pending) SetRegister(r rune) {
	p.Register = r
	p.awaitReg = false
}

// PushDigit folds a typed digit into the count. A leading 0 is not a count digit
// (it is the "0" motion); callers must gate that before calling here.
func (p *Pending) PushDigit(d int) { p.Count = p.Count*10 + d }

// SetOperator records the operator key.
func (p *Pending) SetOperator(op rune) { p.Operator = op }

// EffectiveCount returns the accumulated count, treating an unset count as 1.
func (p Pending) EffectiveCount() int {
	if p.Count <= 0 {
		return 1
	}
	return p.Count
}

// Reset clears all pending state after a command commits or is cancelled.
func (p *Pending) Reset() { *p = Pending{} }
