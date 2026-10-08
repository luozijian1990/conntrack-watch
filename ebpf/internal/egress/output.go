package egress

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Output struct {
	encoder *json.Encoder
	close   func() error
}

func NewOutput(c Config) (*Output, error) {
	var writer io.Writer = os.Stdout
	closeFn := func() error { return nil }
	if c.Log.Path != "" {
		if err := os.MkdirAll(filepath.Dir(c.Log.Path), 0755); err != nil {
			return nil, err
		}
		// Fail startup on a bad path instead of silently falling back to another file.
		f, err := os.OpenFile(c.Log.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
		l := &lumberjack.Logger{Filename: c.Log.Path, MaxSize: c.Log.MaxSizeMB, MaxBackups: c.Log.MaxBackups, MaxAge: c.Log.MaxAgeDays, Compress: true}
		writer = l
		closeFn = l.Close
	}
	return &Output{encoder: json.NewEncoder(writer), close: closeFn}, nil
}
func (o *Output) Write(e Event) error { return o.encoder.Encode(e) }
func (o *Output) Close() error        { return o.close() }
