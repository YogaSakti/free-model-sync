package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// OpenCode Zen gates its free tier on the whole official-client fingerprint:
// a request missing any single part is answered with 403 FreeTierError. The
// probe reaches upstream through host.http.do, which bypasses the executor
// pipeline, so the opencode-enhancer plugin never gets to add the fingerprint
// and a healthy model would look dead and be pruned from config.yaml.
//
// Values follow plugin/fingerprint.go in
// github.com/YogaSakti/cpa-opencode-enhancer, which applies the same
// fingerprint on the path this probe skips.
const (
	openCodeHost          = "opencode.ai"
	openCodeUserAgent     = "opencode/1.18.31" // upstream rejects anything below 1.17
	openCodeClient        = "desktop"
	openCodeProject       = "global"
	openCodeAccept        = "text/event-stream"
	openCodeSessionPrefix = "ses_"
	openCodeRequestPrefix = "msg_"
	base62Alphabet        = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// openCodeTools is the tool quartet the official client always declares.
// Upstream reads its presence as part of the fingerprint.
var openCodeTools = []string{"bash", "glob", "grep", "read"}

// isOpenCodeURL reports whether an endpoint belongs to OpenCode Zen.
func isOpenCodeURL(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == openCodeHost || strings.HasSuffix(host, "."+openCodeHost)
}

// openCodeHeaders returns the client fingerprint for one probe. The request id
// is fresh per call, matching the official client.
func openCodeHeaders() http.Header {
	headers := make(http.Header)
	headers.Set("User-Agent", openCodeUserAgent)
	headers.Set("Accept", openCodeAccept)
	headers.Set("X-Opencode-Client", openCodeClient)
	headers.Set("X-Opencode-Project", openCodeProject)
	headers.Set("X-Opencode-Session", openCodeID(openCodeSessionPrefix))
	headers.Set("X-Opencode-Request", openCodeID(openCodeRequestPrefix))
	return headers
}

// openCodeID renders the official client's id shape: the prefix, the low 48
// bits of the current millisecond as 12 lowercase hex digits, then 14 base62
// characters. The random tail keeps two ids in the same millisecond apart.
func openCodeID(prefix string) string {
	milli := uint64(time.Now().UnixMilli())
	raw := make([]byte, 14)
	if _, err := rand.Read(raw); err != nil {
		// Randomness is a uniqueness aid here, not a security boundary.
		for i := range raw {
			raw[i] = byte(milli >> (8 * (i % 8)))
		}
	}
	var out strings.Builder
	out.WriteString(prefix)
	fmt.Fprintf(&out, "%012x", milli&0xFFFFFFFFFFFF)
	for _, b := range raw {
		out.WriteByte(base62Alphabet[int(b)%len(base62Alphabet)])
	}
	return out.String()
}

// openCodeToolSet declares the tool quartet in Chat Completions shape.
func openCodeToolSet() []chatTool {
	tools := make([]chatTool, 0, len(openCodeTools))
	for _, name := range openCodeTools {
		tools = append(tools, chatTool{
			Type: "function",
			Function: chatToolFunction{
				Name:        name,
				Description: "OpenCode built-in " + name + " tool",
				Parameters:  chatToolParameters{Type: "object", Properties: map[string]any{}},
			},
		})
	}
	return tools
}

// isEventStream reports whether an upstream reply is SSE rather than JSON.
func isEventStream(response *pluginapi.HTTPResponse) bool {
	if strings.Contains(strings.ToLower(response.Headers.Get("Content-Type")), "text/event-stream") {
		return true
	}
	trimmed := strings.TrimLeft(string(response.Body), " \t\r\n")
	return strings.HasPrefix(trimmed, "data:") || strings.HasPrefix(trimmed, "event:")
}

// eventStreamCarriesCompletion reports whether an SSE body holds at least one
// completion chunk. An `error` payload fails even when the status was 200.
func eventStreamCarriesCompletion(body []byte) bool {
	found := false
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk map[string]json.RawMessage
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if _, bad := chunk["error"]; bad {
			return false
		}
		if _, ok := chunk["choices"]; ok {
			found = true
		}
	}
	return found
}
