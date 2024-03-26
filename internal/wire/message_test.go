package wire

import (
	"reflect"
	"testing"
)

func TestWireMessage_Serialize(t *testing.T) {
	w := WireMessage{
		ID:      "testID",
		Payload: []byte("testPayload"),
	}

	got, err := w.Serialize()
	if err != nil {
		t.Errorf("Serialize() error = %v", err)
		return
	}

	want := []byte(`{"ID":"testID","Payload":"dGVzdFBheWxvYWQ="}`)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Serialize() got = %v, want %v", got, want)
	}
}

func TestWireMessage_Deserialize(t *testing.T) {
	bytes := []byte(`{"ID":"testID","Payload":"dGVzdFBheWxvYWQ="}`)
	w := &WireMessage{}

	if err := w.Deserialize(bytes); err != nil {
		t.Errorf("Deserialize() error = %v", err)
		return
	}

	want := &WireMessage{
		ID:      "testID",
		Payload: []byte("testPayload"),
	}
	if !reflect.DeepEqual(w, want) {
		t.Errorf("Deserialize() got = %v, want %v", w, want)
	}
}
