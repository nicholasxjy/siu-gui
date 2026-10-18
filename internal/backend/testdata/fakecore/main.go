// Command fakecore simulates the siu-core sidecar's NDJSON stdio protocol
// for backend package tests, so they do not depend on the real Rust
// binary. Behavior is controlled entirely via environment variables:
//
//	FAKE_CORE_MODE:
//	  "echo"      (default) respond to every request with
//	              {"id":<id>,"ok":true,"data":{"method":<method>,"params":<params>}}
//	  "slow-echo" like echo, but sleeps FAKE_CORE_DELAY_MS first
//	  "hang"      read requests but never respond (for cancellation tests)
//	  "stubborn"  never respond or exit on EOF (for forced-shutdown tests)
//	  "crash-once" on first run (no FAKE_CORE_STATEFILE content yet),
//	              read one line then exit(1) without responding, to
//	              simulate a mid-request crash; on subsequent runs
//	              (after the Client restarts it) behaves like "echo"
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	mode := os.Getenv("FAKE_CORE_MODE")
	if mode == "" {
		mode = "echo"
	}

	reader := bufio.NewReaderSize(os.Stdin, 1<<20)
	writer := bufio.NewWriter(os.Stdout)

	if mode == "crash-once" && !crashOnceAlreadyUsed() {
		markCrashOnceUsed()
		_, _ = reader.ReadString('\n')
		os.Exit(1)
	}

	delay := 0
	if ms := os.Getenv("FAKE_CORE_DELAY_MS"); ms != "" {
		if v, err := strconv.Atoi(ms); err == nil {
			delay = v
		}
	}

	for {
		lineBytes, err := reader.ReadString('\n')
		if lineBytes == "" && err != nil {
			return
		}
		line := strings.TrimSpace(lineBytes)
		if line == "" {
			if err != nil {
				return
			}
			continue
		}

		switch mode {
		case "stubborn":
			for {
				time.Sleep(time.Hour)
			}
		case "hang":
			// intentionally never respond
		case "slow-echo":
			if delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
			respond(writer, line)
		default: // "echo", and "crash-once" after its first-run crash
			respond(writer, line)
		}

		if err != nil {
			return
		}
	}
}

func respond(w *bufio.Writer, requestLine string) {
	id, method, params := parseRequest(requestLine)
	fmt.Fprintf(w, "{\"id\":%d,\"ok\":true,\"data\":{\"method\":%s,\"params\":%s}}\n", id, jsonString(method), params)
	_ = w.Flush()
}

// parseRequest extracts id/method/params from the request line with
// minimal hand-rolled parsing to keep this helper free of non-stdlib
// dependencies and resilient to GO111MODULE=off builds.
func parseRequest(line string) (id int64, method string, paramsRaw string) {
	id = extractIntField(line, "\"id\":")
	method = extractStringField(line, "\"method\":")
	paramsRaw = extractRawObjectField(line, "\"params\":")
	if paramsRaw == "" {
		paramsRaw = "{}"
	}
	return
}

func extractIntField(line, key string) int64 {
	idx := strings.Index(line, key)
	if idx < 0 {
		return 0
	}
	rest := line[idx+len(key):]
	end := 0
	for end < len(rest) && (rest[end] == '-' || (rest[end] >= '0' && rest[end] <= '9')) {
		end++
	}
	v, _ := strconv.ParseInt(rest[:end], 10, 64)
	return v
}

func extractStringField(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(key):]
	rest = strings.TrimLeft(rest, " \t")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func extractRawObjectField(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(key):]
	rest = strings.TrimLeft(rest, " \t")
	if len(rest) == 0 || rest[0] != '{' {
		return ""
	}
	depth := 0
	for i, ch := range rest {
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[:i+1]
			}
		}
	}
	return "{}"
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func crashOnceAlreadyUsed() bool {
	path := os.Getenv("FAKE_CORE_STATEFILE")
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "1"
}

func markCrashOnceUsed() {
	path := os.Getenv("FAKE_CORE_STATEFILE")
	if path == "" {
		return
	}
	_ = os.WriteFile(path, []byte("1"), 0o600)
}
