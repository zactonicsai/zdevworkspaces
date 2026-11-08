package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// table prints rows with aligned columns, kubectl style.
func table(w io.Writer, header []string, rows [][]string) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	line := func(cells []string) {
		var sb strings.Builder
		for i, c := range cells {
			if i == len(cells)-1 {
				sb.WriteString(c)
			} else {
				sb.WriteString(c + strings.Repeat(" ", widths[i]-len(c)+2))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(sb.String(), " "))
	}
	line(header)
	for _, r := range rows {
		line(r)
	}
}

// kv prints a two-column key/value block.
func kv(w io.Writer, pairs [][2]string) {
	width := 0
	for _, p := range pairs {
		if len(p[0]) > width {
			width = len(p[0])
		}
	}
	for _, p := range pairs {
		fmt.Fprintf(w, "%-*s  %s\n", width, p[0], p[1])
	}
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func joinOrDash(xs []string) string {
	if len(xs) == 0 {
		return "-"
	}
	return strings.Join(xs, ",")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// age renders an RFC 3339 timestamp as "3h", "2d" …, like kubectl.
func age(ts string) string {
	if ts == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
