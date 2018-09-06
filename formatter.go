package stackdriver

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-stack/stack"
	"github.com/sirupsen/logrus"
)

var skipTimestamp bool

type severity string

var (
	DefaultSubjectKey     = "X-Subject-Id"
	DefaultOperationIdKey = "X-Request-Id"
)

const (
	// Copied from https://github.com/opentracing/basictracer-go/blob/master/propagation_ot.go
	// Might be necessary to keep this in sync.
	prefixTracerState = "ot-tracer-"

	fieldNameTraceID = prefixTracerState + "traceid"
	fieldNameSpanID  = prefixTracerState + "spanid"
)

const (
	severityDebug    severity = "DEBUG"
	severityInfo     severity = "INFO"
	severityWarning  severity = "WARNING"
	severityError    severity = "ERROR"
	severityCritical severity = "CRITICAL"
	severityAlert    severity = "ALERT"
)

var levelsToSeverity = map[logrus.Level]severity{
	logrus.DebugLevel: severityDebug,
	logrus.InfoLevel:  severityInfo,
	logrus.WarnLevel:  severityWarning,
	logrus.ErrorLevel: severityError,
	logrus.FatalLevel: severityCritical,
	logrus.PanicLevel: severityAlert,
}

type serviceContext struct {
	Service string `json:"service,omitempty"`
	Version string `json:"version,omitempty"`
}

type reportLocation struct {
	FilePath     string `json:"filePath,omitempty"`
	LineNumber   int    `json:"lineNumber,omitempty"`
	FunctionName string `json:"functionName,omitempty"`
}

type operation struct {
	Id       string `json:"id,omitempty"`
	Producer string `json:"producer,omitempty"`
	First    *bool  `json:"first,omitempty"`
	Last     *bool  `json:"last,omitempty"`
}

type sourceLocation struct {
	File     string `json:"file,omitempty"`
	Line     string `json:"line,omitempty"`
	Function string `json:"function,omitempty"`
}

type context struct {
	Data           map[string]interface{} `json:"data,omitempty"`
	ReportLocation *reportLocation        `json:"reportLocation,omitempty"`
	HTTPRequest    map[string]interface{} `json:"httpRequest,omitempty"`
	User           string                 `json:"user,omitempty"`
}

type entry struct {
	Timestamp      string          `json:"timestamp,omitempty"`
	ServiceContext *serviceContext `json:"serviceContext,omitempty"`
	Message        string          `json:"message,omitempty"`
	Severity       severity        `json:"severity,omitempty"`
	Context        *context        `json:"context,omitempty"`
	Trace          string          `json:"logging.googleapis.com/trace,omitempty"`
	SpanID         string          `json:"logging.googleapis.com/span_id,omitempty"`
	SourceLocation *sourceLocation `json:"sourceLocation,omitempty"`
	Operation      *operation      `json:"operation,omitempty"`
}

// Formatter implements Stackdriver formatting for logrus.
type Formatter struct {
	Service   string
	Version   string
	StackSkip []string
}

// Option lets you configure the Formatter.
type Option func(*Formatter)

// WithService lets you configure the service name used for error reporting.
func WithService(n string) Option {
	return func(f *Formatter) {
		f.Service = n
	}
}

// WithVersion lets you configure the service version used for error reporting.
func WithVersion(v string) Option {
	return func(f *Formatter) {
		f.Version = v
	}
}

// WithStackSkip lets you configure which packages should be skipped for locating the error.
func WithStackSkip(v string) Option {
	return func(f *Formatter) {
		f.StackSkip = append(f.StackSkip, v)
	}
}

// NewFormatter returns a new Formatter.
func NewFormatter(options ...Option) *Formatter {
	fmtr := Formatter{
		StackSkip: []string{
			"github.com/sirupsen/logrus",
		},
	}
	for _, option := range options {
		option(&fmtr)
	}
	return &fmtr
}

func (f *Formatter) errorOrigin() (stack.Call, error) {
	skip := func(pkg string) bool {
		for _, skip := range f.StackSkip {
			if pkg == skip {
				return true
			}
		}
		return false
	}

	// We start at 2 to skip this call and our caller's call.
	for i := 2; ; i++ {
		c := stack.Caller(i)
		// ErrNoFunc indicates we're over traversing the stack.
		if _, err := c.MarshalText(); err != nil {
			return stack.Call{}, nil
		}
		pkg := fmt.Sprintf("%+k", c)
		// Remove vendoring from package path.
		parts := strings.SplitN(pkg, "/vendor/", 2)
		pkg = parts[len(parts)-1]
		if !skip(pkg) {
			return c, nil
		}
	}
}

// Format formats a logrus entry according to the Stackdriver specifications.
func (f *Formatter) Format(e *logrus.Entry) ([]byte, error) {
	severity := levelsToSeverity[e.Level]

	ee := entry{

		Message:  e.Message,
		Severity: severity,
		Context: &context{
			Data: e.Data,
		},
	}

	if !skipTimestamp {
		ee.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	switch severity {
	case severityError, severityCritical, severityAlert:
		ee.ServiceContext = &serviceContext{
			Service: f.Service,
			Version: f.Version,
		}

		// When using WithError(), the error is sent separately, but Error
		// Reporting expects it to be a part of the message so we append it
		// instead.
		if err, ok := ee.Context.Data["error"]; ok {
			ee.Message = fmt.Sprintf("%s: %s", e.Message, err)
			delete(ee.Context.Data, "error")
		} else {
			ee.Message = e.Message
		}

		// As a convenience, when using supplying the httpRequest field, it
		// gets special care.
		if reqData, ok := ee.Context.Data["httpRequest"]; ok {
			if req, ok := reqData.(map[string]interface{}); ok {
				ee.Context.HTTPRequest = req
				delete(ee.Context.Data, "httpRequest")
			}
		}

		// If we find a user/subject id in the log fields, add it to the error context
		if user := getStringValue(DefaultSubjectKey, ee.Context.Data); user != "" {
			ee.Context.User = user
			delete(ee.Context.Data, DefaultSubjectKey)
		}

		// Extract report location from call stack.
		if c, err := f.errorOrigin(); err == nil {
			lineNumber, _ := strconv.ParseInt(fmt.Sprintf("%d", c), 10, 64)

			ee.Context.ReportLocation = &reportLocation{
				FilePath:     fmt.Sprintf("%+s", c),
				LineNumber:   int(lineNumber),
				FunctionName: fmt.Sprintf("%n", c),
			}
		}
	default:
		// Always try to add the source location to logs, if we are not reporting an error
		if c, err := f.errorOrigin(); err == nil {
			lineNumber, _ := strconv.ParseInt(fmt.Sprintf("%d", c), 10, 64)

			ee.SourceLocation = &sourceLocation{
				File:     fmt.Sprintf("%+s", c),
				Line:     fmt.Sprintf("%d", int(lineNumber)),
				Function: fmt.Sprintf("%n", c),
			}
		}
	}

	if operationId := getStringValue(DefaultOperationIdKey, ee.Context.Data); operationId != "" {
		ee.Operation = &operation{
			Id: operationId,
		}
		delete(ee.Context.Data, DefaultOperationIdKey)
	}

	// Add tracing information to all logs if available
	if traceId := getStringValue(fieldNameTraceID, ee.Context.Data); traceId != "" {
		ee.Trace = traceId
		delete(ee.Context.Data, fieldNameTraceID)
	}
	if spanId := getStringValue(fieldNameSpanID, ee.Context.Data); spanId != "" {
		ee.SpanID = spanId
		delete(ee.Context.Data, fieldNameSpanID)
	}

	b, err := json.Marshal(ee)
	if err != nil {
		return nil, err
	}

	return append(b, '\n'), nil
}

func getStringValue(key string, context map[string]interface{}) string {
	if val, ok := context[key]; ok {
		if stringVal, ok := val.(string); ok {
			return stringVal
		}
	}
	return ""
}
