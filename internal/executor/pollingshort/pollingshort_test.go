package pollingshort

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/iw2rmb/amata/internal/executor"
	exprruntime "github.com/iw2rmb/amata/internal/expr"
	"github.com/iw2rmb/amata/internal/spec"
	"github.com/iw2rmb/amata/internal/state"
)

type scriptedResponse struct {
	status  int
	headers map[string][]string
	body    string
	err     error
}

type observedCall struct {
	Method       string
	URL          string
	Body         string
	Headers      map[string]string
	HasDeadline  bool
	DeadlineIn   time.Duration
	RequestIndex int
}

type scriptedDoer struct {
	t         *testing.T
	responses []scriptedResponse
	calls     []observedCall
}

func (d *scriptedDoer) Do(req *http.Request) (*http.Response, error) {
	d.t.Helper()

	body := ""
	if req.Body != nil {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			d.t.Fatalf("read request body: %v", err)
		}
		body = string(data)
	}

	call := observedCall{
		Method:       req.Method,
		URL:          req.URL.String(),
		Body:         body,
		Headers:      firstHeaderValues(req.Header),
		RequestIndex: len(d.calls),
	}
	if deadline, ok := req.Context().Deadline(); ok {
		call.HasDeadline = true
		call.DeadlineIn = time.Until(deadline)
	}
	d.calls = append(d.calls, call)

	if len(d.calls)-1 >= len(d.responses) {
		d.t.Fatalf("unexpected request %d %s %s", len(d.calls), req.Method, req.URL.String())
	}
	response := d.responses[len(d.calls)-1]
	if response.err != nil {
		return nil, response.err
	}

	headers := http.Header{}
	for key, values := range response.headers {
		for _, value := range values {
			headers.Add(key, value)
		}
	}

	return &http.Response{
		StatusCode: response.status,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(response.body)),
	}, nil
}

func TestPollingShortExecute(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		configure  func(fields map[string]any)
		responses  []scriptedResponse
		wantStatus state.StepStatus
		wantCode   string
		wantCalls  int
		wantWaits  []time.Duration
		assert     func(t *testing.T, result state.StepResult, doer *scriptedDoer)
	}{
		{
			name: "request plus confirm succeeds and intervals apply only between later attempts",
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{status: 200, body: `{"state":"pending","ok":false}`},
				{status: 200, body: `{"state":"done","ok":true}`},
			},
			wantStatus: state.StepStatusSucceeded,
			wantCalls:  3,
			wantWaits:  []time.Duration{5 * time.Second},
			assert: func(t *testing.T, result state.StepResult, doer *scriptedDoer) {
				t.Helper()

				if got := doer.calls[0].Method; got != "POST" {
					t.Fatalf("request method = %q, want POST", got)
				}
				if got := doer.calls[1].Method; got != "GET" {
					t.Fatalf("confirm method = %q, want GET", got)
				}
				if got := doer.calls[1].URL; got != "https://poll.test/confirm/42" {
					t.Fatalf("first confirm url = %q, want https://poll.test/confirm/42", got)
				}

				value, ok := result.Value.(map[string]any)
				if !ok {
					t.Fatalf("result value type = %T, want map[string]any", result.Value)
				}
				confirm, ok := value["confirm"].(map[string]any)
				if !ok {
					t.Fatalf("confirm type = %T, want map[string]any", value["confirm"])
				}
				if got := confirm["attempts"]; got != 2 {
					t.Fatalf("confirm attempts = %#v, want 2", got)
				}
			},
		},
		{
			name: "confirm timeout uses wall clock budget",
			configure: func(fields map[string]any) {
				confirm := fields["confirm"].(map[string]any)
				confirm["timeout"] = "4s"
			},
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{status: 200, body: `{"state":"pending","ok":false}`},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "confirm_timeout",
			wantCalls:  2,
			wantWaits:  []time.Duration{4 * time.Second},
		},
		{
			name: "request transport failure",
			responses: []scriptedResponse{
				{err: errors.New("dial failed")},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "request_failed",
			wantCalls:  1,
			wantWaits:  []time.Duration{},
		},
		{
			name: "request status failure",
			responses: []scriptedResponse{
				{status: 503, body: `{"error":"unavailable"}`},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "request_http_status",
			wantCalls:  1,
			wantWaits:  []time.Duration{},
		},
		{
			name: "confirm transport failure",
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{err: errors.New("connection reset")},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "confirm_failed",
			wantCalls:  2,
			wantWaits:  []time.Duration{},
		},
		{
			name: "confirm status failure",
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{status: 500, body: `{"state":"failed"}`},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "confirm_http_status",
			wantCalls:  2,
			wantWaits:  []time.Duration{},
		},
		{
			name: "done true and success false fails polling_unsuccessful",
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{status: 200, body: `{"state":"done","ok":false}`},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "polling_unsuccessful",
			wantCalls:  2,
			wantWaits:  []time.Duration{},
		},
		{
			name: "done_when must resolve to boolean",
			configure: func(fields map[string]any) {
				fields["done_when"] = `'nope'`
			},
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{status: 200, body: `{"state":"done","ok":true}`},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "invalid_done_when",
			wantCalls:  2,
			wantWaits:  []time.Duration{},
		},
		{
			name: "success_when must resolve to boolean on each confirm result",
			configure: func(fields map[string]any) {
				fields["done_when"] = `$.value.confirm.attempts > 100`
				fields["success_when"] = `'nope'`
			},
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{status: 200, body: `{"state":"pending","ok":false}`},
			},
			wantStatus: state.StepStatusFailed,
			wantCode:   "invalid_success_when",
			wantCalls:  2,
			wantWaits:  []time.Duration{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runDir := t.TempDir()
			fields := basePollingStepFields()
			if tc.configure != nil {
				tc.configure(fields)
			}

			doer := &scriptedDoer{t: t, responses: tc.responses}
			now := time.Unix(0, 0)
			waits := []time.Duration{}
			exec := &Executor{
				httpClient: doer,
				now:        func() time.Time { return now },
				wait: func(_ context.Context, duration time.Duration) error {
					waits = append(waits, duration)
					now = now.Add(duration)
					return nil
				},
				checkpoints: fileCheckpointStore{},
			}

			result := exec.Execute(context.Background(), newPollingStepContext(runDir, fields))

			if result.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (error=%#v)", result.Status, tc.wantStatus, result.Error)
			}
			if tc.wantCode != "" {
				if result.Error == nil || result.Error.Code != tc.wantCode {
					t.Fatalf("error = %#v, want code %q", result.Error, tc.wantCode)
				}
			}
			if got := len(doer.calls); got != tc.wantCalls {
				t.Fatalf("http calls = %d, want %d", got, tc.wantCalls)
			}
			if !reflect.DeepEqual(waits, tc.wantWaits) {
				t.Fatalf("waits = %#v, want %#v", waits, tc.wantWaits)
			}
			if tc.assert != nil {
				tc.assert(t, result, doer)
			}
		})
	}
}

func TestPollingShortCheckpointStartupBranches(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		prepare    func(t *testing.T, key executor.CheckpointKey, started time.Time)
		responses  []scriptedResponse
		wantStatus state.StepStatus
		wantCode   string
		wantCalls  int
		assert     func(t *testing.T, result state.StepResult, doer *scriptedDoer)
	}{
		{
			name: "no checkpoint executes request then confirm",
			responses: []scriptedResponse{
				{status: 201, body: `{"confirm_url":"https://poll.test/confirm/42"}`},
				{status: 200, body: `{"state":"done","ok":true}`},
			},
			wantStatus: state.StepStatusSucceeded,
			wantCalls:  2,
			assert: func(t *testing.T, _ state.StepResult, doer *scriptedDoer) {
				t.Helper()
				if got := doer.calls[0].URL; got != "https://poll.test/request" {
					t.Fatalf("request url = %q, want https://poll.test/request", got)
				}
			},
		},
		{
			name: "valid non-terminal checkpoint resumes confirm without request",
			prepare: func(t *testing.T, key executor.CheckpointKey, started time.Time) {
				t.Helper()
				mustSaveCheckpoint(t, key, pollState{
					Request:        requestResponse("https://poll.test/confirm/42"),
					Confirm:        confirmState{},
					ConfirmStarted: started,
				})
			},
			responses: []scriptedResponse{
				{status: 200, body: `{"state":"done","ok":true}`},
			},
			wantStatus: state.StepStatusSucceeded,
			wantCalls:  1,
			assert: func(t *testing.T, _ state.StepResult, doer *scriptedDoer) {
				t.Helper()
				if got := doer.calls[0].URL; got != "https://poll.test/confirm/42" {
					t.Fatalf("confirm url = %q, want https://poll.test/confirm/42", got)
				}
			},
		},
		{
			name: "valid terminal checkpoint returns terminal result without extra HTTP",
			prepare: func(t *testing.T, key executor.CheckpointKey, started time.Time) {
				t.Helper()
				poll := pollState{
					Request: requestResponse("https://poll.test/confirm/42"),
					Confirm: confirmState{
						Attempts:    1,
						Response:    &httpResponse{Status: 200, Headers: map[string][]string{}, Value: map[string]any{"state": "done", "ok": true}},
						LastAttempt: started,
					},
					ConfirmStarted: started,
					Terminal:       terminalFromResult(executor.Succeeded(map[string]any{"ready": true})),
				}
				mustSaveCheckpoint(t, key, poll)
			},
			responses:  []scriptedResponse{},
			wantStatus: state.StepStatusSucceeded,
			wantCalls:  0,
			assert: func(t *testing.T, result state.StepResult, _ *scriptedDoer) {
				t.Helper()
				value, ok := result.Value.(map[string]any)
				if !ok {
					t.Fatalf("result value type = %T, want map[string]any", result.Value)
				}
				if got := value["ready"]; got != true {
					t.Fatalf("result value[ready] = %#v, want true", got)
				}
			},
		},
		{
			name: "malformed checkpoint fails invalid_checkpoint",
			prepare: func(t *testing.T, key executor.CheckpointKey, _ time.Time) {
				t.Helper()
				path := checkpointPath(key)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("create checkpoint dir: %v", err)
				}
				if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
					t.Fatalf("write malformed checkpoint: %v", err)
				}
			},
			responses:  []scriptedResponse{},
			wantStatus: state.StepStatusFailed,
			wantCode:   "invalid_checkpoint",
			wantCalls:  0,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runDir := t.TempDir()
			fields := basePollingStepFields()
			stepCtx := newPollingStepContext(runDir, fields)
			key := checkpointKey(stepCtx)
			started := time.Unix(123, 0)
			if tc.prepare != nil {
				tc.prepare(t, key, started)
			}

			doer := &scriptedDoer{t: t, responses: tc.responses}
			now := started
			exec := &Executor{
				httpClient:  doer,
				now:         func() time.Time { return now },
				wait:        func(_ context.Context, duration time.Duration) error { now = now.Add(duration); return nil },
				checkpoints: fileCheckpointStore{},
			}

			result := exec.Execute(context.Background(), stepCtx)

			if result.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q (error=%#v)", result.Status, tc.wantStatus, result.Error)
			}
			if tc.wantCode != "" {
				if result.Error == nil || result.Error.Code != tc.wantCode {
					t.Fatalf("error = %#v, want code %q", result.Error, tc.wantCode)
				}
			}
			if got := len(doer.calls); got != tc.wantCalls {
				t.Fatalf("http calls = %d, want %d", got, tc.wantCalls)
			}
			if tc.assert != nil {
				tc.assert(t, result, doer)
			}
		})
	}
}

func TestPollingShortConfirmRequestTimeoutBoundedByRemainingBudget(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	fields := basePollingStepFields()
	confirm := fields["confirm"].(map[string]any)
	confirm["timeout"] = "2s"
	confirm["request_timeout"] = "10s"

	stepCtx := newPollingStepContext(runDir, fields)
	key := checkpointKey(stepCtx)
	started := time.Unix(1_000, 0)
	mustSaveCheckpoint(t, key, pollState{
		Request:        requestResponse("https://poll.test/confirm/42"),
		Confirm:        confirmState{},
		ConfirmStarted: started,
	})

	doer := &scriptedDoer{t: t, responses: []scriptedResponse{{status: 200, body: `{"state":"done","ok":true}`}}}
	now := started.Add(1500 * time.Millisecond)
	exec := &Executor{
		httpClient:  doer,
		now:         func() time.Time { return now },
		wait:        func(context.Context, time.Duration) error { return nil },
		checkpoints: fileCheckpointStore{},
	}

	result := exec.Execute(context.Background(), stepCtx)
	if result.Status != state.StepStatusSucceeded {
		t.Fatalf("status = %q, want succeeded (error=%#v)", result.Status, result.Error)
	}
	if got := len(doer.calls); got != 1 {
		t.Fatalf("http calls = %d, want 1", got)
	}
	if !doer.calls[0].HasDeadline {
		t.Fatalf("confirm request has no deadline")
	}
	if doer.calls[0].DeadlineIn <= 0 {
		t.Fatalf("confirm deadline duration = %s, want > 0", doer.calls[0].DeadlineIn)
	}
	if doer.calls[0].DeadlineIn > 1200*time.Millisecond {
		t.Fatalf("confirm deadline duration = %s, want bounded by remaining budget", doer.calls[0].DeadlineIn)
	}
}

func TestDecodeResponseBodyDeterministic(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
		want any
	}{
		{
			name: "whitespace body decodes to nil",
			body: "  \n\t  ",
			want: nil,
		},
		{
			name: "valid json decodes to parsed value",
			body: `{"status":"ok","count":2}`,
			want: map[string]any{"status": "ok", "count": float64(2)},
		},
		{
			name: "invalid json remains raw string",
			body: "not-json",
			want: "not-json",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			decoded, err := decodeResponseBody([]byte(tc.body))
			if err != nil {
				t.Fatalf("decodeResponseBody() error = %v", err)
			}
			if !reflect.DeepEqual(decoded, tc.want) {
				t.Fatalf("decoded = %#v, want %#v", decoded, tc.want)
			}
		})
	}
}

func basePollingStepFields() map[string]any {
	return map[string]any{
		"request": map[string]any{
			"url": "https://poll.test/request",
		},
		"confirm": map[string]any{
			"url":             "{{ ctx.value.request.response.value.confirm_url }}",
			"interval":        "5s",
			"timeout":         "10s",
			"request_timeout": "2s",
		},
		"done_when":    `$.value.confirm.response.value.state == "done"`,
		"success_when": `$.value.confirm.response.value.ok`,
	}
}

func newPollingStepContext(runDir string, fields map[string]any) executor.StepContext {
	return executor.StepContext{
		RunDir:    runDir,
		FrameID:   "frame-000001",
		StepIndex: 2,
		Step: spec.Step{
			ID:     "poll-step",
			Type:   "polling.short",
			Fields: fields,
		},
		Runtime: exprruntime.NewRuntime(map[string]any{"ctx": map[string]any{}}),
	}
}

func requestResponse(confirmURL string) httpResponse {
	return httpResponse{
		Status:  201,
		Headers: map[string][]string{},
		Value: map[string]any{
			"confirm_url": confirmURL,
		},
	}
}

func mustSaveCheckpoint(t *testing.T, key executor.CheckpointKey, poll pollState) {
	t.Helper()
	if err := (fileCheckpointStore{}).Save(key, poll); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
}

func firstHeaderValues(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make(map[string]string, len(headers))
	for _, key := range keys {
		headerValues := headers.Values(key)
		if len(headerValues) == 0 {
			values[key] = ""
			continue
		}
		values[key] = headerValues[0]
	}

	return values
}
