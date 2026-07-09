package httphandler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSETextIncludesSubtype(t *testing.T) {
	rr := httptest.NewRecorder()
	emitter := newSSEEmitter(rr)

	emitter.Text("你好")

	body := rr.Body.String()
	if !strings.Contains(body, "event: text") {
		t.Fatalf("body missing text event:\n%s", body)
	}
	if !strings.Contains(body, `"content":"你好"`) || !strings.Contains(body, `"subtype":"final"`) {
		t.Fatalf("text payload missing content/subtype:\n%s", body)
	}
}

func TestSSEEventUsesTypedEventNameAndJSONPayload(t *testing.T) {
	rr := httptest.NewRecorder()
	emitter := newSSEEmitter(rr)

	emitter.Event("thinking_tips", `{"status":"start","type":"msg","text":"小租正在思考"}`)

	body := rr.Body.String()
	if !strings.Contains(body, "event: thinking_tips") {
		t.Fatalf("body missing typed event:\n%s", body)
	}
	if !strings.Contains(body, `"status":"start"`) || strings.Contains(body, `"detail"`) {
		t.Fatalf("event payload should be direct JSON, got:\n%s", body)
	}
}

func TestSSELegacyEventUsesGenericEventWrapper(t *testing.T) {
	rr := httptest.NewRecorder()
	emitter := newSSEEmitterWithVersion(rr, true)

	emitter.Event("thinking_tips", `{"status":"start"}`)

	body := rr.Body.String()
	if !strings.Contains(body, "event: event") || !strings.Contains(body, `"name":"thinking_tips"`) {
		t.Fatalf("legacy body =\n%s", body)
	}
}
