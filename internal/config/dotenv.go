package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv reads path (default ".env") and sets variables that are not already
// present in the process environment. Missing file is ignored.
func LoadDotEnv(path string) error {
	if path == "" {
		path = ".env"
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if lineNo == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s:%d: empty key", path, lineNo)
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
	}
	return sc.Err()
}
