package config

import (
	"runtime"
	"strings"
)

// shortHostName drops the ".local" macOS puts at the end of its hostname for
// Bonjour: no part of the name anyone gave the computer, and noise wherever
// the viewer shows it.
func shortHostName(name string) string {
	const bonjour = ".local"
	if len(name) > len(bonjour) && strings.EqualFold(name[len(name)-len(bonjour):], bonjour) {
		return name[:len(name)-len(bonjour)]
	}
	return name
}

// fallbackHostName names the computer when the system will not say its name.
func fallbackHostName() string {
	if runtime.GOOS == "darwin" {
		return "Mac Host"
	}
	return "Windows Host"
}
