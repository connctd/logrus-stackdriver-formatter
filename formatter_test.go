package stackdriver

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/kr/pretty"

	"github.com/sirupsen/logrus"
)

func TestFormatter(t *testing.T) {
	for _, tt := range []struct {
		run func(*logrus.Logger)
		out map[string]interface{}
	}{
		{
			run: func(logger *logrus.Logger) {
				logger.WithField("foo", "bar").Info("my log entry")
			},
			out: map[string]interface{}{
				"severity": "INFO",
				"message":  "my log entry",
				"foo":      "bar",
			},
		},
		{
			run: func(logger *logrus.Logger) {
				logger.WithField("foo", "bar").Error("my log entry")
			},
			out: map[string]interface{}{
				"severity": "ERROR",
				"message":  "my log entry",
				"foo":      "bar",
				"serviceContext": map[string]interface{}{
					"service": "test",
					"version": "0.1",
				},
				"context": map[string]interface{}{
					"reportLocation": map[string]interface{}{
						"filePath":     "github.com/TV4/logrus-stackdriver-formatter/formatter_test.go",
						"lineNumber":   118.0,
						"functionName": "TestFormatter",
					},
				},
			},
		},
		{
			run: func(logger *logrus.Logger) {
				logger.
					WithField("foo", "bar").
					WithError(errors.New("test error")).
					Error("my log entry")
			},
			out: map[string]interface{}{
				"severity": "ERROR",
				"message":  "my log entry: test error",
				"foo":      "bar",
				"serviceContext": map[string]interface{}{
					"service": "test",
					"version": "0.1",
				},
				"context": map[string]interface{}{
					"reportLocation": map[string]interface{}{
						"filePath":     "github.com/TV4/logrus-stackdriver-formatter/formatter_test.go",
						"lineNumber":   118.0,
						"functionName": "TestFormatter",
					},
				},
			},
		},
		{
			run: func(logger *logrus.Logger) {
				logger.
					WithFields(logrus.Fields{
						"foo": "bar",
						"context": map[string]interface{}{
							"httpRequest": map[string]interface{}{
								"method": "GET",
							},
						},
					}).
					Error("my log entry")
			},
			out: map[string]interface{}{
				"severity": "ERROR",
				"message":  "my log entry",
				"foo":      "bar",
				"serviceContext": map[string]interface{}{
					"service": "test",
					"version": "0.1",
				},
				"context": map[string]interface{}{
					"httpRequest": map[string]interface{}{
						"method": "GET",
					},
					"reportLocation": map[string]interface{}{
						"filePath":     "github.com/TV4/logrus-stackdriver-formatter/formatter_test.go",
						"lineNumber":   118.0,
						"functionName": "TestFormatter",
					},
				},
			},
		},
	} {
		var out bytes.Buffer

		logger := logrus.New()
		logger.Out = &out
		logger.Formatter = NewFormatter(
			WithService("test"),
			WithVersion("0.1"),
		)

		tt.run(logger)

		var got map[string]interface{}
		json.Unmarshal(out.Bytes(), &got)

		if !reflect.DeepEqual(got, tt.out) {
			t.Errorf("unexpected output = %# v; want = %# v", pretty.Formatter(got), pretty.Formatter(tt.out))
		}
	}
}