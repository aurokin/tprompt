package daemon

// Wire envelope for the daemon IPC protocol. One wireRequest per connection,
// one wireResponse back, then close. Envelope-based dispatch means a future
// request type (e.g. shutdown) can land without breaking the protocol.
//
// The envelope is package-private — callers use the Client interface, which
// hides the framing.
const (
	kindSubmit = "submit"
	kindStatus = "status"
)

type wireRequest struct {
	Kind   string         `json:"kind"`
	Submit *SubmitRequest `json:"submit,omitempty"`
	Status *StatusRequest `json:"status,omitempty"`
}

type wireResponse struct {
	Kind   string          `json:"kind,omitempty"`
	Error  string          `json:"error,omitempty"`
	Submit *SubmitResponse `json:"submit,omitempty"`
	Status *StatusResponse `json:"status,omitempty"`
}
