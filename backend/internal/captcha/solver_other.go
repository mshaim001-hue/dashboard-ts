//go:build !darwin

package captcha

func solveMacVision(_ []byte) (string, error) {
	return "", errNoVision
}

func ensureTesseract() error {
	return nil
}

var errNoVision = errorString("vision not available")

type errorString string

func (e errorString) Error() string { return string(e) }
