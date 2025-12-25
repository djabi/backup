package backup

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Properties map[string]string

func LoadProperties(path string) (Properties, error) {
	props := make(Properties)
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return props, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			// Handle basic escaping for spaces in keys which Java Properties does
			key := strings.TrimSpace(parts[0])
			key = strings.ReplaceAll(key, "\\ ", " ")

			value := strings.TrimSpace(parts[1])
			props[key] = value
		}
	}
	return props, scanner.Err()
}

func (p Properties) Store(path string, comments string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	if comments != "" {
		fmt.Fprintf(file, "#%s\n", comments)
	}
	// TODO: Add timestamp like Java does? Not strictly necessary.

	// Sort keys for deterministic output
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// Escape spaces in key
		escapedKey := strings.ReplaceAll(k, " ", "\\ ")
		fmt.Fprintf(file, "%s=%s\n", escapedKey, p[k])
	}
	return nil
}
