package pollingshort

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type httpRequestSpec struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    any
}

type httpResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Value   any                 `json:"value"`
}

func performHTTPRequest(ctx context.Context, doer httpDoer, spec httpRequestSpec) (httpResponse, error) {
	bodyBytes, err := encodeRequestBody(spec.Body)
	if err != nil {
		return httpResponse{}, err
	}

	var reader io.Reader
	if bodyBytes != nil {
		reader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, spec.Method, spec.URL, reader)
	if err != nil {
		return httpResponse{}, err
	}

	setRequestHeaders(req.Header, spec.Headers)

	resp, err := doer.Do(req)
	if err != nil {
		return httpResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpResponse{}, fmt.Errorf("read response body: %w", err)
	}

	decoded, err := decodeResponseBody(body)
	if err != nil {
		return httpResponse{}, err
	}

	return httpResponse{
		Status:  resp.StatusCode,
		Headers: cloneHeaders(resp.Header),
		Value:   decoded,
	}, nil
}

func setRequestHeaders(headers http.Header, values map[string]string) {
	if len(values) == 0 {
		return
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		headers.Set(key, values[key])
	}
}

func encodeRequestBody(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}

	switch typed := value.(type) {
	case string:
		return []byte(typed), nil
	case []byte:
		cloned := make([]byte, len(typed))
		copy(cloned, typed)
		return cloned, nil
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
		return payload, nil
	}
}

func decodeResponseBody(body []byte) (any, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err == nil {
		return decoded, nil
	}

	return string(body), nil
}

func cloneHeaders(headers http.Header) map[string][]string {
	if len(headers) == 0 {
		return map[string][]string{}
	}

	cloned := make(map[string][]string, len(headers))
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := headers.Values(key)
		copyValues := make([]string, len(values))
		copy(copyValues, values)
		cloned[key] = copyValues
	}

	return cloned
}
