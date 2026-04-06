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

	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(event)
}
