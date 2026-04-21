package progress

import (
	"encoding/json"
	"io"
	"sync"
)

type jsonlController struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func NewJSONLController(writer io.Writer) Sink {
	if writer == nil {
		return nil
	}

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return &jsonlController{enc: encoder}
}

func (c *jsonlController) WriteProgress(event Event) {
	if c == nil || c.enc == nil {
		return
	}

	event = compactEventForJSONL(event)

	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(event)
}

func compactEventForJSONL(event Event) Event {
	switch event.Kind {
	case EventStepStarted, EventStepFinished:
		event.Snapshot.Active = nil
		event.Snapshot.Steps = nil
	}
	return event
}
