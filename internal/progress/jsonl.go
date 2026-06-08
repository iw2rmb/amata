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
	case EventStepStarted, EventStepFinished, EventRunFinished:
		event.Snapshot.Active = nil
		event.Snapshot.Steps = nil
	}
	if event.Kind == EventStepFinished {
		event.Step = compactFinishedStepForJSONL(event.Step)
	}
	return event
}

func compactFinishedStepForJSONL(step *Step) *Step {
	if step == nil {
		return nil
	}

	compacted := cloneStep(*step)
	if isAgentStepType(compacted.Type) && compacted.Descriptor != nil {
		compacted.Descriptor.DetailText = nil
	}
	return &compacted
}
