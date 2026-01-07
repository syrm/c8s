package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Common timestamp formats for parsing log timestamps
var timestampFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

// formatJSONLog parses a JSON log line and formats it for display.
// Returns the formatted line and whether parsing was successful.
func formatJSONLog(line string, showTimestamp bool) (string, bool) {
	jsonStart := strings.Index(line, "{")
	if jsonStart == -1 {
		return "", false
	}

	dockerTimestamp := strings.TrimSpace(line[:jsonStart])
	jsonPart := line[jsonStart:]

	var logData map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &logData); err != nil {
		return "", false
	}

	var logTimestamp, level, msg string
	var extras []string

	// Extract timestamp fields
	for _, key := range []string{"time", "timestamp", "ts", "@timestamp", "t"} {
		if v, ok := logData[key]; ok {
			logTimestamp = fmt.Sprintf("%v", v)
			delete(logData, key)
			break
		}
	}

	// Extract level fields
	for _, key := range []string{"level", "lvl", "severity", "loglevel"} {
		if v, ok := logData[key]; ok {
			level = strings.ToUpper(fmt.Sprintf("%v", v))
			delete(logData, key)
			break
		}
	}

	// Extract message fields
	for _, key := range []string{"msg", "message", "text"} {
		if v, ok := logData[key]; ok {
			msg = fmt.Sprintf("%v", v)
			delete(logData, key)
			break
		}
	}

	// Collect remaining fields sorted by key
	var keys []string
	for k := range logData {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := logData[k]
		vStr := fmt.Sprintf("%v", v)
		vStr = strings.ReplaceAll(vStr, "[", "[[]")
		extras = append(extras, fmt.Sprintf("[blue]%s[-]=%s", k, vStr))
	}

	// Build formatted line
	var parts []string

	if showTimestamp {
		if logTimestamp != "" {
			formattedTs := formatTimestamp(logTimestamp)
			parts = append(parts, fmt.Sprintf("[gray]%s[-]", formattedTs))
		} else if dockerTimestamp != "" {
			parts = append(parts, fmt.Sprintf("[gray]%s[-]", dockerTimestamp))
		}
	}

	if level != "" {
		levelColor := getLevelColor(level)
		parts = append(parts, fmt.Sprintf("[%s]%-5s[-]", levelColor, level))
	}
	if msg != "" {
		msg = strings.ReplaceAll(msg, "[", "[[]")
		parts = append(parts, msg)
	}
	if len(extras) > 0 {
		parts = append(parts, strings.Join(extras, " "))
	}

	return strings.Join(parts, " ") + "\n", true
}

// formatTimestamp converts various timestamp formats to RFC3339Nano.
func formatTimestamp(ts string) string {
	for _, format := range timestampFormats {
		if t, err := time.Parse(format, ts); err == nil {
			return t.Format(time.RFC3339Nano)
		}
	}
	return ts
}

// getLevelColor returns the tview color for a log level.
func getLevelColor(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL":
		return "red"
	case "WARN", "WARNING":
		return "yellow"
	case "INFO":
		return "green"
	case "DEBUG", "TRACE":
		return "gray"
	default:
		return "white"
	}
}

// extractTimestampPrefix tries to extract a timestamp from the beginning of a string.
// Returns the timestamp, the remaining string, and whether a timestamp was found.
func extractTimestampPrefix(s string) (timestamp string, rest string, found bool) {
	if len(s) < 20 {
		return "", s, false
	}

	spaceIdx := -1
	for i := 19; i < len(s) && i < 40; i++ {
		if s[i] == ' ' {
			spaceIdx = i
			break
		}
	}

	if spaceIdx == -1 {
		return "", s, false
	}

	potentialTs := s[:spaceIdx]

	for _, format := range timestampFormats {
		if _, err := time.Parse(format, potentialTs); err == nil {
			return potentialTs, s[spaceIdx+1:], true
		}
	}

	return "", s, false
}

// colorizeLogLine applies color formatting to a log line based on its content.
func colorizeLogLine(line string, showTimestamp bool) string {
	// Try to parse as JSON first
	if formatted, ok := formatJSONLog(line, showTimestamp); ok {
		return formatted
	}

	// For non-JSON logs, handle timestamps properly
	dockerTs, afterDockerTs, hasDockerTs := extractTimestampPrefix(line)
	logTs, content, hasLogTs := extractTimestampPrefix(afterDockerTs)

	var displayLine string
	if showTimestamp {
		if hasLogTs {
			formattedTs := formatTimestamp(logTs)
			displayLine = formattedTs + " " + content
		} else if hasDockerTs {
			displayLine = dockerTs + " " + afterDockerTs
		} else {
			displayLine = line
		}
	} else {
		if hasLogTs {
			displayLine = content
		} else if hasDockerTs {
			displayLine = afterDockerTs
		} else {
			displayLine = line
		}
	}

	lower := strings.ToLower(displayLine)
	displayLine = strings.ReplaceAll(displayLine, "[", "[[]")

	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic"):
		return "[red]" + displayLine + "[-]"
	case strings.Contains(lower, "warn"):
		return "[yellow]" + displayLine + "[-]"
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		return "[gray]" + displayLine + "[-]"
	case strings.Contains(lower, "info"):
		return "[green]" + displayLine + "[-]"
	default:
		return displayLine
	}
}
