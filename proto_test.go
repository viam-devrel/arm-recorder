package armrecorder

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

func TestReadingsMapIsProtoSafe(t *testing.T) {
	// joints must be []interface{}, not []float64
	if _, err := structpb.NewStruct(map[string]interface{}{
		"joints": toInterfaceSlice([]float64{0.1, -0.2, 0.3}),
	}); err != nil {
		t.Fatalf("joints not proto-encodable: %v", err)
	}
}

func TestSessionsListIsProtoSafe(t *testing.T) {
	if _, err := structpb.NewStruct(map[string]interface{}{
		"sessions": toStringInterfaceSlice([]string{"a", "b"}),
	}); err != nil {
		t.Fatalf("sessions not proto-encodable: %v", err)
	}
}
