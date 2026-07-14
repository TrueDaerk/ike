package langgo

import "ike/internal/lang"

var _ lang.RunCommandProvider = toolchain{}

// RunCommand implements lang.RunCommandProvider (0350, #575): `go run
// file.go` with the resolved go binary (PATH fallback matches the detector).
func (toolchain) RunCommand(_ string, spec lang.RunSpec, interpreter string) ([]string, bool) {
	if interpreter == "" {
		interpreter = "go"
	}
	return append([]string{interpreter, "run", spec.File}, spec.Args...), true
}
