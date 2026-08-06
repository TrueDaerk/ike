package editor

// secrets.go is the editor half of secret masking (#1623): a language's span
// producer marks the value of a secret-suspect key (dotenv's `*_TOKEN`,
// `*_SECRET`, `PASSWORD`, …) with a stand-in span carrying secret.Mask, and
// the shared conceal layer of #1585 draws that mask instead of the value. The
// caret reveals positionally (#1594) — put the cursor in the value and the raw
// secret is there to read and edit, move away and it masks again — so a screen
// share or a colleague behind you never sees a credential the reader did not
// deliberately look at.
//
// The family gates on editor.secret_masking / view.toggleSecretMasking,
// independent of the decode families and of the markdown/log layers.

// toggleSecretMasking flips masking for this view. Like the other view
// toggles, the override sticks: applyConfig stops tracking the config default
// once toggled.
func (m *Model) toggleSecretMasking() {
	m.secretMask = !m.secretMask
	m.secretMaskSet = true
}
