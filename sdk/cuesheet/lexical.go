package cuesheet

import (
	"math"
	"path"
	"strconv"
	"strings"
)

func tokenize(line string) ([]string, bool) {
	var tokens []string
	for line != "" {
		line = strings.TrimLeft(line, " \t")
		if line == "" {
			break
		}
		if line[0] == '"' {
			end := strings.IndexByte(line[1:], '"')
			if end < 0 {
				return nil, false
			}
			tokens = append(tokens, line[1:end+1])
			line = line[end+2:]
			if line != "" && line[0] != ' ' && line[0] != '\t' {
				return nil, false
			}
		} else {
			end := strings.IndexAny(line, " \t")
			if end < 0 {
				end = len(line)
			}
			if strings.ContainsRune(line[:end], '"') {
				return nil, false
			}
			tokens = append(tokens, line[:end])
			line = line[end:]
		}
	}
	return tokens, len(tokens) > 0
}

func relativeReference(value string) (string, bool) {
	value = strings.ReplaceAll(value, "\\", "/")
	if value == "" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return "", false
	}
	if strings.ContainsAny(value, ":*?<>|\"") {
		return "", false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			continue
		}
		if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return "", false
		}
		base, _, _ := strings.Cut(strings.ToUpper(segment), ".")
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return "", false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return "", false
		}
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func position(value string) (int64, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || len(parts[0]) < 2 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return 0, false
	}
	var numbers [3]int64
	for i, part := range parts {
		if !decimal(part) {
			return 0, false
		}
		number, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return 0, false
		}
		numbers[i] = number
	}
	if numbers[1] >= 60 || numbers[2] >= FramesPerSecond {
		return 0, false
	}
	remainder := numbers[1]*FramesPerSecond + numbers[2]
	if numbers[0] > (math.MaxInt64-remainder)/(60*FramesPerSecond) {
		return 0, false
	}
	return numbers[0]*60*FramesPerSecond + remainder, true
}
