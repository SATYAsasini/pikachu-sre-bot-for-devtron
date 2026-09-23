package config

import (
	"bufio"
	"os"
	"strings"
)

// loadDotEnv reads a .env file into the process environment.
//
// Without this the binary only sees variables the shell happened to export,
// so `make run` worked while running the built binary directly did not — the
// key was sitting in .env being ignored. Values already present in the real
// environment always win, so a deployment's own configuration is never
// overridden by a file that happens to be lying around.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // absent is the normal case in a container
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		// Strip one layer of matching quotes, which people add out of habit
		// and which would otherwise become part of the secret.
		if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
		if key == "" {
			continue
		}
		if _, already := os.LookupEnv(key); already {
			continue
		}
		_ = os.Setenv(key, val)
	}
}
