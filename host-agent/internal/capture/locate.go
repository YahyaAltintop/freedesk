package capture

import (
	"os"
	"path/filepath"
	"runtime"
)

// resolveBinary decides which ffmpeg to run: an explicit path wins; otherwise
// the ffmpeg shipped with the agent (see bundledBinary); otherwise "ffmpeg"
// from PATH.
func resolveBinary(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if exe, err := os.Executable(); err == nil {
		if found := bundledBinary(filepath.Dir(exe)); found != "" {
			return found
		}
	}
	return defaultFFmpegBinary
}

// bundledBinary looks for the ffmpeg that ships with the agent in dir, the
// folder the executable is in: first in its ffmpeg subfolder, how the release
// zip is laid out, then right next to it, how older zips were and where
// someone copying a build by hand tends to put it.
func bundledBinary(dir string) string {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, candidate := range []string{
		filepath.Join(dir, "ffmpeg", name),
		filepath.Join(dir, name),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
