package keepalive

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestKeepAliveService(t *testing.T) {
	ctx := context.Background()

	svc := NewServer()
	assert.NotNil(t, svc)

	done := make(chan error, 1)
	go func() {
		done <- svc.Start(ctx)
	}()

	assert.Eventually(t, func() bool { return svc.started.Load() }, time.Second, 10*time.Millisecond)
	assert.NoError(t, svc.Stop(ctx))

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive server did not return from Start")
	}
}
