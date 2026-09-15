package capture

import (
	"os"
	"path/filepath"
	"runtime"
)

// resolveBinary decides which ffmpeg to run: an explicit path wins; otherwise
// an ffmpeg shipped next to the agent executable (how releases are packaged);
// otherwise "ffmpeg" from PATH.
func resolveBinary(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if exe, err := os.Executable(); err == nil {
		name := "ffmpeg"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidate := filepath.Join(filepath.Dir(exe), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return defaultFFmpegBinary
}
