package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

var (
	sessionIDPattern = regexp.MustCompile(`^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
	requestIDPattern = regexp.MustCompile(`^msg_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
)

type probeBody struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
	Tools  []struct {
		Type     string `json:"type"`
		Function struct {
			Name       string `json:"name"`
			Parameters struct {
				Type string `json:"type"`
			} `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
}

// stubUpstream replaces the host callback with one answering every probe the
// same way, and records the last request the plugin sent.
func stubUpstream(t *testing.T, response pluginapi.HTTPResponse) *hostHTTPRequest {
	t.Helper()
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	var got hostHTTPRequest
	hostCall = func(_ string, request any, result any) error {
		got = request.(hostHTTPRequest)
		*result.(*pluginapi.HTTPResponse) = response
		return nil
	}
	return &got
}

func eventStreamResponse() pluginapi.HTTPResponse {
	return pluginapi.HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n" +
			"data: [DONE]\n\n"),
	}
}

func TestOpenCodeProbeSendsClientFingerprint(t *testing.T) {
	got := stubUpstream(t, eventStreamResponse())

	response := callModelTestEndpoint(t, []byte(`{
		"api_key":"bearer-key",
		"base_url":"https://opencode.ai/zen/v1",
		"model":"mimo-v2.5-free"
	}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}

	if ua := got.Headers.Get("User-Agent"); ua != "opencode/1.18.31" {
		t.Errorf("User-Agent = %q", ua)
	}
	if client := got.Headers.Get("X-Opencode-Client"); client != "desktop" {
		t.Errorf("X-Opencode-Client = %q", client)
	}
	if project := got.Headers.Get("X-Opencode-Project"); project != "global" {
		t.Errorf("X-Opencode-Project = %q", project)
	}
	if accept := got.Headers.Get("Accept"); accept != "text/event-stream" {
		t.Errorf("Accept = %q", accept)
	}
	if session := got.Headers.Get("X-Opencode-Session"); !sessionIDPattern.MatchString(session) {
		t.Errorf("X-Opencode-Session = %q", session)
	}
	if requestID := got.Headers.Get("X-Opencode-Request"); !requestIDPattern.MatchString(requestID) {
		t.Errorf("X-Opencode-Request = %q", requestID)
	}

	var payload probeBody
	if err := json.Unmarshal(got.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Stream {
		t.Error("stream = false, want true")
	}
	names := make(map[string]string, len(payload.Tools))
	for _, tool := range payload.Tools {
		if tool.Type != "function" || tool.Function.Parameters.Type != "object" {
			t.Errorf("tool = %#v", tool)
		}
		names[tool.Function.Name] = tool.Type
	}
	for _, want := range []string{"bash", "glob", "grep", "read"} {
		if _, ok := names[want]; !ok {
			t.Errorf("tools missing %q, got %v", want, names)
		}
	}
}

func TestOpenCodeProbeUsesAFreshRequestIDPerCall(t *testing.T) {
	got := stubUpstream(t, eventStreamResponse())
	body := []byte(`{"base_url":"https://opencode.ai/zen/v1","model":"mimo-v2.5-free"}`)

	callModelTestEndpoint(t, body)
	first := got.Headers.Get("X-Opencode-Request")
	callModelTestEndpoint(t, body)
	second := got.Headers.Get("X-Opencode-Request")

	if first == "" || first == second {
		t.Fatalf("request ids = %q and %q, want two different ids", first, second)
	}
}

func TestProbeOmitsUnresolvedHeaderReferences(t *testing.T) {
	got := stubUpstream(t, eventStreamResponse())

	callModelTestEndpoint(t, []byte(`{
		"base_url":"https://opencode.ai/zen/v1",
		"headers":{"X-Opencode-Session":"$X-Opencode-Session","X-Literal":"kept"},
		"model":"mimo-v2.5-free"
	}`))

	if session := got.Headers.Get("X-Opencode-Session"); !sessionIDPattern.MatchString(session) {
		t.Errorf("X-Opencode-Session = %q, want a generated id rather than the reference", session)
	}
	if literal := got.Headers.Get("X-Literal"); literal != "kept" {
		t.Errorf("X-Literal = %q, want the literal custom header preserved", literal)
	}
}

func TestPlanOmitsUnresolvedHeaderReferences(t *testing.T) {
	got := stubUpstream(t, pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[]}`)})

	planManagementUpdate("callback-1", []byte(`{
		"base_url":"https://openrouter.ai/api/v1",
		"headers":{"X-Reference":"$X-Reference","X-Literal":"kept"}
	}`))

	if reference, ok := got.Headers["X-Reference"]; ok {
		t.Errorf("X-Reference = %q, want the unresolved reference dropped", reference)
	}
	if literal := got.Headers.Get("X-Literal"); literal != "kept" {
		t.Errorf("X-Literal = %q", literal)
	}
}

func TestProbeTreatsRateLimitedModelAsReachable(t *testing.T) {
	stubUpstream(t, pluginapi.HTTPResponse{
		StatusCode: http.StatusTooManyRequests,
		Body:       []byte(`{"type":"error","error":{"type":"FreeUsageLimitError","message":"daily limit"}}`),
	})

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/zen/v1","model":"mimo-v2.5-free"}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
}

func TestProbeFailsUnavailableModel(t *testing.T) {
	for name, upstream := range map[string]pluginapi.HTTPResponse{
		"model is unavailable": {
			StatusCode: http.StatusBadRequest,
			Body:       []byte(`{"error":{"message":"Model is unavailable"}}`),
		},
		"endpoint is unavailable": {
			StatusCode: http.StatusServiceUnavailable,
			Body:       []byte(`{"error":{"message":"Endpoint is unavailable"}}`),
		},
		"free tier gate": {
			StatusCode: http.StatusForbidden,
			Body:       []byte(`{"type":"error","error":{"type":"FreeTierError","message":"OpenCode's free tier can only be used from within OpenCode"}}`),
		},
	} {
		t.Run(name, func(t *testing.T) {
			stubUpstream(t, upstream)
			response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/zen/v1","model":"gone-free"}`))
			// A dead model is a result, not a gateway failure: answering 5xx
			// lets a proxy replace the body with its own error page, and the
			// page then cannot say why the model failed.
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
			}
			var verdict struct {
				OK     bool   `json:"ok"`
				Reason string `json:"reason"`
				Status int    `json:"status"`
			}
			if err := json.Unmarshal(response.Body, &verdict); err != nil {
				t.Fatal(err)
			}
			if verdict.OK || verdict.Reason == "" {
				t.Fatalf("verdict = %#v", verdict)
			}
			if verdict.Status != upstream.StatusCode {
				t.Errorf("status = %d, want the upstream status %d", verdict.Status, upstream.StatusCode)
			}
		})
	}
}

func TestProbeAcceptsEventStreamResponse(t *testing.T) {
	stubUpstream(t, eventStreamResponse())

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/zen/v1","model":"mimo-v2.5-free"}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
}

func TestProbeRejectsEmptyEventStream(t *testing.T) {
	stubUpstream(t, pluginapi.HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       []byte("data: [DONE]\n\n"),
	})

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/zen/v1","model":"mimo-v2.5-free"}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	var verdict struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(response.Body, &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.OK {
		t.Fatal("an event stream without a completion chunk must not pass")
	}
}

func TestProbeReportsAnUnreachableProviderAsAResult(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	hostCall = func(string, any, any) error { return errors.New("dial tcp: no such host") }

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.invalid/v1","model":"mimo-v2.5-free"}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	var verdict struct {
		OK     bool   `json:"ok"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(response.Body, &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.OK || verdict.Reason == "" {
		t.Fatalf("verdict = %#v", verdict)
	}
	if strings.Contains(verdict.Reason, "no such host") {
		t.Errorf("reason = %q, want no upstream detail", verdict.Reason)
	}
}

func TestNonOpenCodeProbeKeepsTheJSONShape(t *testing.T) {
	got := stubUpstream(t, pluginapi.HTTPResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"choices":[{"message":{"content":"OK"}}]}`),
	})

	response := callModelTestEndpoint(t, []byte(`{
		"base_url":"https://openrouter.ai/api/v1",
		"model":"example/free"
	}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if ua := got.Headers.Get("User-Agent"); ua != openAICompatUserAgent {
		t.Errorf("User-Agent = %q", ua)
	}
	if accept := got.Headers.Get("Accept"); accept != "application/json" {
		t.Errorf("Accept = %q", accept)
	}
	for _, header := range []string{"X-Opencode-Client", "X-Opencode-Project", "X-Opencode-Session", "X-Opencode-Request"} {
		if value := got.Headers.Get(header); value != "" {
			t.Errorf("%s = %q, want no fingerprint on a non-OpenCode provider", header, value)
		}
	}
	var payload probeBody
	if err := json.Unmarshal(got.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Stream || len(payload.Tools) != 0 {
		t.Errorf("payload = %#v, want the unchanged non-streaming probe", payload)
	}
}

func TestOpenCodeIDCarriesTheOfficialShape(t *testing.T) {
	seen := make(map[string]struct{}, 200)
	for i := 0; i < 100; i++ {
		session := openCodeID(openCodeSessionPrefix)
		request := openCodeID(openCodeRequestPrefix)
		if !sessionIDPattern.MatchString(session) {
			t.Fatalf("session id = %q", session)
		}
		if !requestIDPattern.MatchString(request) {
			t.Fatalf("request id = %q", request)
		}
		for _, id := range []string{session, request} {
			if _, clash := seen[id]; clash {
				t.Fatalf("id %q repeated", id)
			}
			seen[id] = struct{}{}
		}
	}
}

func TestOpenCodeHeadersDeclareEveryGate(t *testing.T) {
	headers := openCodeHeaders()
	for name, want := range map[string]string{
		"User-Agent":         "opencode/1.18.31",
		"Accept":             "text/event-stream",
		"X-Opencode-Client":  "desktop",
		"X-Opencode-Project": "global",
	} {
		if got := headers.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if !sessionIDPattern.MatchString(headers.Get("X-Opencode-Session")) {
		t.Errorf("X-Opencode-Session = %q", headers.Get("X-Opencode-Session"))
	}
	if !requestIDPattern.MatchString(headers.Get("X-Opencode-Request")) {
		t.Errorf("X-Opencode-Request = %q", headers.Get("X-Opencode-Request"))
	}
}

func TestIsOpenCodeURL(t *testing.T) {
	for endpoint, want := range map[string]bool{
		"https://opencode.ai/zen/v1/chat/completions": true,
		"https://api.opencode.ai/v1/chat/completions": true,
		"https://OpenCode.ai/zen/v1":                  true,
		"https://openrouter.ai/api/v1":                false,
		"https://notopencode.ai/v1":                   false,
		"https://opencode.ai.example.com/v1":          false,
		"":                                            false,
	} {
		if got := isOpenCodeURL(endpoint); got != want {
			t.Errorf("isOpenCodeURL(%q) = %t, want %t", endpoint, got, want)
		}
	}
}

func TestClassifyProbeAcceptsEveryRateLimit(t *testing.T) {
	throttled := pluginapi.HTTPResponse{
		StatusCode: http.StatusTooManyRequests,
		Body:       []byte(`{"type":"error","error":{"type":"FreeUsageLimitError"}}`),
	}
	verdict := classifyProbe(&throttled)
	if !verdict.OK || verdict.Unavailable {
		t.Fatalf("verdict = %#v, want a rate-limited model to pass", verdict)
	}
}

// stubUpstreamSequence answers each host call from fn, recording every request.
func stubUpstreamSequence(t *testing.T, fn func(hostHTTPRequest) (pluginapi.HTTPResponse, error)) *[]hostHTTPRequest {
	t.Helper()
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	var seen []hostHTTPRequest
	hostCall = func(_ string, request any, result any) error {
		call := request.(hostHTTPRequest)
		seen = append(seen, call)
		response, err := fn(call)
		if err != nil {
			return err
		}
		*result.(*pluginapi.HTTPResponse) = response
		return nil
	}
	return &seen
}

func TestPlanFallsBackToTheOpenCodeCatalogURL(t *testing.T) {
	seen := stubUpstreamSequence(t, func(call hostHTTPRequest) (pluginapi.HTTPResponse, error) {
		if call.URL == "https://opencode.ai/inference/v1/models" {
			return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"mimo-v2.5-free"},{"id":"paid"}]}`)}, nil
		}
		return pluginapi.HTTPResponse{StatusCode: http.StatusNotFound, Body: []byte(`{"error":"not found"}`)}, nil
	})

	response := planManagementUpdate("callback-1", []byte(`{
		"api_key":"oc_sk_key",
		"base_url":"https://opencode.ai/inference/openai/v1"
	}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	var plan planResponse
	if err := json.Unmarshal(response.Body, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.CatalogURL != "https://opencode.ai/inference/v1/models" {
		t.Errorf("catalog_url = %q", plan.CatalogURL)
	}
	if len(plan.Free) != 1 || plan.Free[0] != "mimo-v2.5-free" {
		t.Errorf("free = %v", plan.Free)
	}
	if len(*seen) != 2 || (*seen)[0].URL != "https://opencode.ai/inference/openai/v1/models" {
		t.Errorf("requests = %v", *seen)
	}
}

func TestPlanKeepsOneCatalogURLForOtherProviders(t *testing.T) {
	seen := stubUpstreamSequence(t, func(hostHTTPRequest) (pluginapi.HTTPResponse, error) {
		return pluginapi.HTTPResponse{StatusCode: http.StatusNotFound}, nil
	})

	response := planManagementUpdate("callback-1", []byte(`{"base_url":"https://openrouter.ai/api/v1"}`))
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %v, want a single attempt for a non-OpenCode provider", *seen)
	}
}

func TestProbeSeparatesDeadModelsFromInconclusiveOnes(t *testing.T) {
	type want struct {
		unavailable bool
	}
	cases := map[string]struct {
		upstream pluginapi.HTTPResponse
		want     want
	}{
		"unauthorized":            {pluginapi.HTTPResponse{StatusCode: http.StatusUnauthorized}, want{true}},
		"free tier gate":          {pluginapi.HTTPResponse{StatusCode: http.StatusForbidden}, want{true}},
		"not found":               {pluginapi.HTTPResponse{StatusCode: http.StatusNotFound}, want{true}},
		"model is unavailable":    {pluginapi.HTTPResponse{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"message":"Model is unavailable"}}`)}, want{true}},
		"model is not supported":  {pluginapi.HTTPResponse{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"message":"Model is not supported"}}`)}, want{true}},
		"responses only model":    {pluginapi.HTTPResponse{StatusCode: http.StatusServiceUnavailable, Body: []byte(`{"error":{"message":"Endpoint is unavailable"}}`)}, want{true}},
		"unexplained bad request": {pluginapi.HTTPResponse{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"message":"try again"}}`)}, want{false}},
		"server error":            {pluginapi.HTTPResponse{StatusCode: http.StatusInternalServerError}, want{false}},
		"transient unavailable":   {pluginapi.HTTPResponse{StatusCode: http.StatusServiceUnavailable, Body: []byte(`{"error":{"message":"upstream is busy"}}`)}, want{false}},
		"bad gateway":             {pluginapi.HTTPResponse{StatusCode: http.StatusBadGateway}, want{false}},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			stubUpstream(t, testCase.upstream)
			response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/inference/openai/v1","model":"candidate-free"}`))
			if response.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
			}
			var verdict struct {
				OK          bool `json:"ok"`
				Unavailable bool `json:"unavailable"`
			}
			if err := json.Unmarshal(response.Body, &verdict); err != nil {
				t.Fatal(err)
			}
			if verdict.OK {
				t.Fatal("want a failed verdict")
			}
			if verdict.Unavailable != testCase.want.unavailable {
				t.Errorf("unavailable = %t, want %t", verdict.Unavailable, testCase.want.unavailable)
			}
		})
	}
}

func TestProbeNeverCallsAnUnreachableProviderDead(t *testing.T) {
	previous := hostCall
	t.Cleanup(func() { hostCall = previous })
	hostCall = func(string, any, any) error { return errors.New("dial tcp: no such host") }

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/inference/openai/v1","model":"mimo-v2.5-free"}`))
	var verdict struct {
		OK          bool `json:"ok"`
		Unavailable bool `json:"unavailable"`
	}
	if err := json.Unmarshal(response.Body, &verdict); err != nil {
		t.Fatal(err)
	}
	if verdict.OK || verdict.Unavailable {
		t.Fatalf("verdict = %#v, a transport failure says nothing about the model", verdict)
	}
}

func TestProbeAcceptsAReasoningOnlyFirstDelta(t *testing.T) {
	stubUpstream(t, pluginapi.HTTPResponse{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"\",\"reasoning\":\"thinking\"}}]}\n\n" +
			"data: [DONE]\n\n"),
	})

	response := callModelTestEndpoint(t, []byte(`{"base_url":"https://opencode.ai/inference/openai/v1","model":"nemotron-3-ultra-free"}`))
	var verdict struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(response.Body, &verdict); err != nil {
		t.Fatal(err)
	}
	if !verdict.OK {
		t.Fatalf("body = %s, a reasoning-only delta still proves the model answered", response.Body)
	}
}
