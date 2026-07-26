// Package progress carries best-effort, user-visible stage notifications
// through context without coupling domain handlers to HTTP or SSE.
package progress

import "context"

type Event struct {
	Code string
	Text string
}

type Reporter func(Event)

type reporterKey struct{}

func WithReporter(ctx context.Context, reporter Reporter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, reporterKey{}, reporter)
}

// Emit reports a transient stage update when the caller installed a reporter.
// Progress is observational only and must not affect domain execution.
func Emit(ctx context.Context, code, text string) {
	if ctx == nil {
		return
	}
	reporter, _ := ctx.Value(reporterKey{}).(Reporter)
	if reporter != nil {
		reporter(Event{Code: code, Text: text})
	}
}
