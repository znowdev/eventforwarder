package wire

import "encoding/json"

type WireMessage struct {
	ID      string
	Payload []byte
}

func (w WireMessage) Serialize() ([]byte, error) {
	return json.Marshal(w)
}

func (w *WireMessage) Deserialize(bytes []byte) error {
	return json.Unmarshal(bytes, w)
}
