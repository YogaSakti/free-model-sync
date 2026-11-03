package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// These requests exercise the management route and capture only its external
// HTTP transport, so stale configured headers cannot bypass the real probe.
func TestOpenCodeV2ProbeOwnsItsFingerprint(t *testing.T) {
	got := stubUpstream(t, eventStreamResponse())
	response := callModelTestEndpoint(t, []byte(`{
		"api_key":"probe-key","base_url":"https://opencode.ai/inference/openai/v1","model":"mimo-v2.5-free",
		"headers":{"User-Agent":"opencode/1.18.31","Accept":"text/plain","X-Opencode-Client":"desktop","X-Opencode-Project":"global","X-Opencode-Session":"stale","X-Opencode-Session-Id":"stale","X-Session-Affinity":"stale","X-Session-Id":"stale","X-Opencode-Request":"msg_stale","B3":"stale","Traceparent":"stale","X-Custom":"kept"}
	}`))
	if response.StatusCode != http.StatusOK || !strings.Contains(string(response.Body), `"ok":true`) {
		t.Fatalf("probe failed: %d %s", response.StatusCode, response.Body)
	}
	for name, want := range map[string]string{
		"User-Agent": "opencode/latest/2.2.5/cli", "Accept": "application/json, text/event-stream",
		"X-Opencode-Client": "cli", "Authorization": "Bearer probe-key", "X-Custom": "kept",
	} {
		if value := got.Headers.Get(name); value != want {
			t.Errorf("%s = %q, want %q", name, value, want)
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	project := sha1.Sum([]byte("opencode-project\x00" + cwd))
	if value := got.Headers.Get("X-Opencode-Project"); value != hex.EncodeToString(project[:]) {
		t.Errorf("project = %q, want the current workspace identity", value)
	}
	session := got.Headers.Get("X-Opencode-Session")
	if !sessionIDPattern.MatchString(session) {
		t.Errorf("invalid session %q", session)
	}
	for _, name := range []string{"X-Opencode-Session-Id", "X-Session-Affinity", "X-Session-Id"} {
		if got.Headers.Get(name) != session {
			t.Errorf("%s must share the generated session", name)
		}
	}
	if value := got.Headers.Get("X-Opencode-Request"); value != "" {
		t.Errorf("obsolete request id leaked: %q", value)
	}
	b3 := regexp.MustCompile(`^([0-9a-f]{32})-([0-9a-f]{16})-1-[0-9a-f]{16}$`).FindStringSubmatch(got.Headers.Get("B3"))
	if len(b3) != 3 {
		t.Errorf("invalid b3 trace %q", got.Headers.Get("B3"))
	} else if got.Headers.Get("Traceparent") != "00-"+b3[1]+"-"+b3[2]+"-01" {
		t.Errorf("traceparent does not match b3: %q", got.Headers.Get("Traceparent"))
	}
}

func TestOpenCodeV2ProbeSendsMinimalAgentEnvironmentAndCoreTools(t *testing.T) {
	for _, model := range []string{"mimo-v2.5-free", "muse-spark-1.2-contributor-free"} {
		t.Run(model, func(t *testing.T) {
			got := stubUpstream(t, eventStreamResponse())
			callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/inference/openai/v1","model":"`+model+`"}`))
			var body map[string]any
			if err := json.Unmarshal(got.Body, &body); err != nil {
				t.Fatal(err)
			}
			cwd, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			wantEnvironment := "You are a coding agent.\n<env>\nWorking directory: " + cwd + "\n</env>"
			muse := strings.HasPrefix(model, "muse-")
			if muse {
				if body["instructions"] != wantEnvironment || body["max_output_tokens"] != float64(128) || body["store"] != false {
					t.Errorf("Responses environment/budget/store = %#v", body)
				}
				if got.URL != "https://opencode.ai/inference/openai/v1/responses" {
					t.Errorf("Muse endpoint = %q", got.URL)
				}
			} else {
				messages := body["messages"].([]any)
				if len(messages) != 2 || messages[0].(map[string]any)["content"] != wantEnvironment || body["max_tokens"] != float64(128) {
					t.Errorf("Chat environment/budget = %#v", body)
				}
				if !reflect.DeepEqual(body["stream_options"], map[string]any{"include_usage": true}) {
					t.Errorf("stream_options = %#v", body["stream_options"])
				}
			}
			tools, ok := body["tools"].([]any)
			if !ok || len(tools) != 5 {
				t.Fatalf("core tools = %#v, want five declarations", body["tools"])
			}
			wantRequired := map[string][]any{
				"edit": {"path", "oldString", "newString"}, "glob": {"pattern"}, "grep": {"pattern"}, "read": {"path"}, "shell": {"command"},
			}
			seen := map[string]bool{}
			for _, raw := range tools {
				tool := raw.(map[string]any)
				if tool["type"] != "function" {
					t.Errorf("tool type = %#v", tool["type"])
				}
				function := tool
				if !muse {
					function = tool["function"].(map[string]any)
				} else if _, nested := tool["function"]; nested {
					t.Error("Responses tools must use flat fields")
				}
				name, _ := function["name"].(string)
				required, known := wantRequired[name]
				if !known || seen[name] {
					t.Errorf("unexpected/duplicate tool %q", name)
				}
				seen[name] = true
				if function["description"] != "Not available in this session. Do not call this tool." || function["strict"] != false {
					t.Errorf("unsafe placeholder declaration for %q: %#v", name, function)
				}
				parameters := function["parameters"].(map[string]any)
				if parameters["type"] != "object" || parameters["additionalProperties"] != false || !reflect.DeepEqual(parameters["required"], required) {
					t.Errorf("%s parameter contract = %#v", name, parameters)
				}
				properties := parameters["properties"].(map[string]any)
				if name == "edit" && !reflect.DeepEqual(properties["replaceAll"], map[string]any{"type": "boolean"}) {
					t.Error("edit must declare optional replaceAll:boolean")
				}
				for _, field := range required {
					property, ok := properties[field.(string)].(map[string]any)
					if !ok || property["type"] != "string" {
						t.Errorf("%s missing typed %s parameter", name, field)
					}
				}
				if _, oldDummy := properties["value"]; oldDummy {
					t.Errorf("%s still exposes the dummy value parameter", name)
				}
			}
		})
	}
}

func TestOpenCodeV2ProbeGeneratesFreshSessionsAndTraces(t *testing.T) {
	got := stubUpstream(t, eventStreamResponse())
	body := []byte(`{"base_url":"https://opencode.ai/inference/openai/v1","model":"mimo-v2.5-free"}`)
	callModelTestEndpoint(t, body)
	session, trace, project := got.Headers.Get("X-Session-Id"), got.Headers.Get("B3"), got.Headers.Get("X-Opencode-Project")
	callModelTestEndpoint(t, body)
	if session == "" || session == got.Headers.Get("X-Session-Id") || trace == "" || trace == got.Headers.Get("B3") {
		t.Fatal("separate probes must use fresh sessions and traces")
	}
	if got.Headers.Get("X-Opencode-Project") != project {
		t.Fatal("separate probes in the same workspace must keep their project identity")
	}
}
