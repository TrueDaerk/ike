package langphp

import "ike/internal/lang"

var _ lang.RunCommandProvider = toolchain{}

// RunCommand implements lang.RunCommandProvider (0350, #575): `php file.php`
// with the resolved interpreter (PATH fallback matches the detector).
func (toolchain) RunCommand(_ string, spec lang.RunSpec, interpreter string) ([]string, bool) {
	if interpreter == "" {
		interpreter = "php"
	}
	return append([]string{interpreter, spec.File}, spec.Args...), true
}
