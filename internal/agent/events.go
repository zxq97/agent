package agent

import "encoding/json"

func emitEventPayload(emit Emitter, name string, payload any) {
	if emit == nil {
		return
	}
	b, _ := json.Marshal(payload)
	emit.Event(name, string(b))
}
