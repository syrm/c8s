package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// timestampFormats contains supported formats for parsing log timestamps.
var timestampFormats = [...]string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

// getStringField extracts the first non-empty string value from a map for the given keys.
func getStringField(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := data[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// formatJSONLog parses a JSON log line and formats it for display.
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

	logTimestamp := getStringField(logData, "time", "timestamp", "ts", "@timestamp", "t")
	level := strings.ToUpper(getStringField(logData, "level", "lvl", "severity", "loglevel"))
	msg := getStringField(logData, "msg", "message", "text")

	knownFields := []string{
		"time", "timestamp", "ts", "@timestamp", "t",
		"level", "lvl", "severity", "loglevel",
		"msg", "message", "text",
	}
	for _, key := range knownFields {
		delete(logData, key)
	}

	var extras []string
	if len(logData) > 0 {
		keys := make([]string, 0, len(logData))
		for k := range logData {
			keys = append(keys, k)
		}
		slices.Sort(keys)

		for _, k := range keys {
			v := logData[k]
			vStr := fmt.Sprintf("%v", v)
			extras = append(extras, logKeyStyle.Render(k)+"="+vStr)
		}
	}

	var builder strings.Builder

	if showTimestamp {
		if logTimestamp != "" {
			formattedTs := formatTimestamp(logTimestamp)
			builder.WriteString(logTimeStyle.Render(formattedTs))
			builder.WriteString(" ")
		} else if dockerTimestamp != "" {
			builder.WriteString(logTimeStyle.Render(dockerTimestamp))
			builder.WriteString(" ")
		}
	}

	if level != "" {
		style := getLevelStyle(level)
		builder.WriteString(style.Render(fmt.Sprintf("%-5s", level)))
		builder.WriteString(" ")
	}

	if msg != "" {
		builder.WriteString(msg)
	}

	if len(extras) > 0 {
		if builder.Len() > 0 {
			builder.WriteString(" ")
		}
		builder.WriteString(strings.Join(extras, " "))
	}

	return builder.String(), true
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

// getLevelStyle returns the lipgloss style for a log level.
func getLevelStyle(level string) lipgloss.Style {
	switch strings.ToUpper(level) {
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL":
		return logErrorStyle
	case "WARN", "WARNING":
		return logWarnStyle
	case "INFO":
		return logInfoStyle
	case "DEBUG", "TRACE":
		return logDebugStyle
	default:
		return lipgloss.NewStyle().Foreground(colorWhite)
	}
}

// extractTimestampPrefix tries to extract a timestamp from the beginning of a string.
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
			displayLine = logTimeStyle.Render(formattedTs) + " " + content
		} else if hasDockerTs {
			displayLine = logTimeStyle.Render(dockerTs) + " " + afterDockerTs
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
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic"):
		return logErrorStyle.Render(displayLine)
	case strings.Contains(lower, "warn"):
		return logWarnStyle.Render(displayLine)
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		return logDebugStyle.Render(displayLine)
	case strings.Contains(lower, "info"):
		return logInfoStyle.Render(displayLine)
	default:
		return displayLine
	}
}
