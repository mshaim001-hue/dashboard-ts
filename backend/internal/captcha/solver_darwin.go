package captcha

import (
	"fmt"
	"os"
	"os/exec"
)

const visionScript = `
import Vision
import AppKit
import Foundation

let path = CommandLine.arguments[1]
let url = URL(fileURLWithPath: path)
guard let img = NSImage(contentsOf: url),
      let tiff = img.tiffRepresentation,
      let bitmap = NSBitmapImageRep(data: tiff),
      let cgImage = bitmap.cgImage else {
    exit(1)
}

let request = VNRecognizeTextRequest()
request.recognitionLevel = .accurate
request.usesLanguageCorrection = false

let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
try handler.perform([request])

var parts: [String] = []
if let results = request.results {
    for obs in results {
        if let top = obs.topCandidates(1).first {
            parts.append(top.string)
        }
    }
}
print(parts.joined().trimmingCharacters(in: .whitespacesAndNewlines))
`

func solveMacVision(img []byte) (string, error) {
	if _, err := exec.LookPath("swift"); err != nil {
		return "", fmt.Errorf("swift not found")
	}

	tmp, err := os.CreateTemp("", "ts-captcha-*.png")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	defer os.Remove(path)

	if _, err := tmp.Write(img); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()

	cmd := exec.Command("swift", "-e", visionScript, path)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("macOS Vision: %w", err)
	}
	return cleanOCR(string(out))
}

func ensureTesseract() error {
	return nil
}
