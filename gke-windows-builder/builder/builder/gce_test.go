package builder

import (
	"context"
	"testing"
)

func TestNewGCEService(t *testing.T) {
	c, err := newGCEService(context.Background())
	if err != nil {
		t.Errorf("cannot create compute client, %s", err)
	}

	if c == nil {
		t.Error("compute client was nil")
	}
}
