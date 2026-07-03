package captcha

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func DecodeImage(data string) ([]byte, error) {
	raw := data
	if strings.HasPrefix(data, "data:") {
		if i := strings.Index(data, ","); i >= 0 {
			raw = data[i+1:]
		}
	}
	return base64.StdEncoding.DecodeString(raw)
}

func Solve(imgData []byte) (string, error) {
	prepared, err := preprocess(imgData)
	if err != nil {
		prepared = imgData
	}

	// 1) system / local tesseract
	if bin := findTesseract(); bin != "" {
		if text, err := runTesseract(bin, prepared); err == nil {
			return text, nil
		}
	}

	// 2) macOS Vision (встроено в систему, brew не нужен)
	if runtime.GOOS == "darwin" {
		if text, err := solveMacVision(prepared); err == nil {
			return text, nil
		}
	}

	return "", fmt.Errorf("OCR не справился — введи captcha в окне Chrome (5 сек)")
}

func runTesseract(bin string, img []byte) (string, error) {
	cmd := exec.Command(
		bin, "stdin", "stdout",
		"--psm", "7",
		"-c", "tessedit_char_whitelist=0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz",
	)
	cmd.Stdin = bytes.NewReader(img)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return cleanOCR(string(out))
}

func cleanOCR(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	text = strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, text)
	if len(text) < 3 {
		return "", fmt.Errorf("OCR слишком короткий: %q", text)
	}
	return text, nil
}

func findTesseract() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".ts-tracker", "bin", "tesseract"),
		"/opt/homebrew/bin/tesseract",
		"/usr/local/bin/tesseract",
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	if p, err := exec.LookPath("tesseract"); err == nil {
		return p
	}
	return ""
}

func preprocess(imgData []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx()*3, bounds.Dy()*3
	out := image.NewRGBA(image.Rect(0, 0, w, h))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := bounds.Min.X + x/3
			sy := bounds.Min.Y + y/3
			r, g, b, _ := img.At(sx, sy).RGBA()
			gray := (r + g + b) / 3
			if gray > 0x8000 {
				out.Set(x, y, color.White)
			} else {
				out.Set(x, y, color.Black)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Notify(title, message string) {
	if runtime.GOOS != "darwin" {
		return
	}
	script := fmt.Sprintf(`display notification %q with title %q`, message, title)
	_ = exec.Command("osascript", "-e", script).Run()
}
