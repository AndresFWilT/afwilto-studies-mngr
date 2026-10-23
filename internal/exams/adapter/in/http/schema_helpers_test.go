package http

import (
	"os"
	"path/filepath"
	"runtime"
	"time"
)

var testNow = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// readSpecFile reads a file of the frontend spec folder of spec 18, which sits
// beside the backend in the lab checkout.
func readSpecFile(name string) ([]byte, error) {
	_, thisFile, _, _ := runtime.Caller(0)
	backend := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..")
	return os.ReadFile(filepath.Join(backend, "..", "..", "frontend", "docs", "specs", "18-exam-simulations", name))
}
