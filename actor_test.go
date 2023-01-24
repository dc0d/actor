//go:generate moq -out callbacks_spy_test.go . Callbacks:CallbacksSpy
// see LICENSE file

// install moq:
// $ go install github.com/matryer/moq@latest

package actor

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	exitVal := m.Run()

	os.Exit(exitVal)
}

func Test_should_call_received_concurrently(t *testing.T) {
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	callbacks.ReceivedFunc = func(T) {}

	Start[T](context.TODO(), mailbox, callbacks)

	mailbox <- 1
	mailbox <- 2
	mailbox <- 3

	assert.Eventually(t, func() bool { return len(callbacks.ReceivedCalls()) == 3 },
		time.Millisecond*300, time.Millisecond*20)

	assert.EqualValues(t, 1, callbacks.ReceivedCalls()[0].IfaceVal)
	assert.EqualValues(t, 2, callbacks.ReceivedCalls()[1].IfaceVal)
	assert.EqualValues(t, 3, callbacks.ReceivedCalls()[2].IfaceVal)
}

func Test_should_call_stopped_when_context_is_canceled(t *testing.T) {
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	ctx, cancel := context.WithCancel(context.Background())
	callbacks.StoppedFunc = func() {}

	Start[T](ctx, mailbox, callbacks)
	cancel()

	assert.Eventually(t, func() bool { return len(callbacks.StoppedCalls()) == 1 },
		time.Millisecond*300, time.Millisecond*20)
}

func Test_should_not_call_received_when_context_is_canceled(t *testing.T) {
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	ctx, cancel := context.WithCancel(context.Background())
	callbacks.StoppedFunc = func() {}

	Start[T](ctx, mailbox, callbacks)
	cancel()

	assert.Eventually(t, func() bool { return len(callbacks.StoppedCalls()) == 1 },
		time.Millisecond*300, time.Millisecond*20)

	sendingStarted := make(chan struct{})
	go func() {
		close(sendingStarted)
		mailbox <- 1
	}()
	<-sendingStarted

	assert.Never(t, func() bool { return len(callbacks.ReceivedCalls()) > 0 },
		time.Millisecond*300, time.Millisecond*20)
}

func Test_should_stop_after_absolute_timeout(t *testing.T) {
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	callbacks.StoppedFunc = func() {}

	Start[T](context.Background(), mailbox, callbacks, WithAbsoluteTimeout(time.Millisecond*50))

	assert.Eventually(t, func() bool { return len(callbacks.StoppedCalls()) == 1 },
		time.Millisecond*300, time.Millisecond*20)
}

func Test_should_stop_when_mailbox_is_closed(t *testing.T) {
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	callbacks.StoppedFunc = func() {}

	Start[T](context.Background(), mailbox, callbacks)
	close(mailbox)

	assert.Eventually(t, func() bool { return len(callbacks.StoppedCalls()) == 1 },
		time.Millisecond*300, time.Millisecond*20)
	assert.Equal(t, 0, len(callbacks.ReceivedCalls()))
}

func Test_should_stop_after_idle_timeout_elapsed(t *testing.T) {
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	callbacks.StoppedFunc = func() {}

	Start[T](context.Background(), mailbox, callbacks, WithIdleTimeout(time.Millisecond*100))

	assert.Never(t, func() bool { return len(callbacks.StoppedCalls()) > 0 },
		time.Millisecond*100, time.Millisecond*20)

	assert.Eventually(t, func() bool { return len(callbacks.StoppedCalls()) == 1 },
		time.Millisecond*300, time.Millisecond*20)
}

func Test_should_respawn_after_receiving_n_messages(t *testing.T) {
	testSignals := newTestSignals()
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	callbacks.StoppedFunc = func() {}
	callbacks.ReceivedFunc = func(T) {}

	Start[T](context.Background(), mailbox, callbacks, WithRespawnAfter(10),
		withStarted(testSignals.started), withStopped(testSignals.stopped))

	go func() {
		for i := 0; i < 20; i++ {
			mailbox <- i
		}
	}()

	assert.Eventually(t, func() bool { return assert.EqualValues(t, 3, testSignals.startCount()) },
		time.Millisecond*300, time.Millisecond*20)
	assert.Eventually(t, func() bool { return assert.EqualValues(t, 2, testSignals.stopCount()) },
		time.Millisecond*300, time.Millisecond*20)
}

func Test_should_not_respawn_if_not_provided(t *testing.T) {
	testSignals := newTestSignals()
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	callbacks.StoppedFunc = func() {}
	callbacks.ReceivedFunc = func(T) {}

	Start[T](context.Background(), mailbox, callbacks,
		withStarted(testSignals.started), withStopped(testSignals.stopped))

	go func() {
		for i := 0; i < 20; i++ {
			mailbox <- i
		}
	}()

	assert.Eventually(t, func() bool { return assert.EqualValues(t, 1, testSignals.startCount()) },
		time.Millisecond*300, time.Millisecond*20)
	assert.Eventually(t, func() bool { return assert.EqualValues(t, 0, testSignals.stopCount()) },
		time.Millisecond*300, time.Millisecond*20)
}

func Test_should_respawn_after_idle_timeout_elapsed_if_respawn_count_is_provided(t *testing.T) {
	testSignals := newTestSignals()
	type T = interface{}
	var (
		mailbox   = make(chan T)
		callbacks = &CallbacksSpy[T]{}
	)
	callbacks.StoppedFunc = func() {}

	Start[T](context.Background(), mailbox, callbacks,
		WithIdleTimeout(time.Millisecond*100), WithRespawnAfter(100),
		withStarted(testSignals.started), withStopped(testSignals.stopped))

	assert.Eventually(t, func() bool { return testSignals.startCount() == 2 },
		time.Millisecond*300, time.Millisecond*20)
	assert.Eventually(t, func() bool { return testSignals.stopCount() == 1 },
		time.Millisecond*300, time.Millisecond*20)

	assert.Never(t, func() bool { return len(callbacks.StoppedCalls()) > 0 },
		time.Millisecond*100, time.Millisecond*20)
}

func Example() {
	mailbox := make(chan any, 3)
	callbacks := &actorHandler{stopped: make(chan struct{})}

	Start[any](context.Background(), mailbox, callbacks)

	mailbox <- 1
	mailbox <- 2
	mailbox <- 3

	close(mailbox)
	<-callbacks.stopped

	// Output:
	// processing message 1
	// processing message 2
	// processing message 3
}

//

type actorHandler struct{ stopped chan struct{} }

func (o *actorHandler) Received(msg any) {
	fmt.Println("processing message", msg)
}

func (o *actorHandler) Stopped() { close(o.stopped) }

//

func newTestSignals() *testSignals {
	return &testSignals{}
}

type testSignals struct {
	startCnt int
	stopCnt  int
	m        sync.RWMutex
}

func (ts *testSignals) startCount() int {
	ts.m.RLock()
	defer ts.m.RUnlock()
	return ts.startCnt
}

func (ts *testSignals) stopCount() int {
	ts.m.RLock()
	defer ts.m.RUnlock()
	return ts.stopCnt
}

func (ts *testSignals) started() {
	ts.m.Lock()
	defer ts.m.Unlock()
	ts.startCnt++
}

func (ts *testSignals) stopped() {
	ts.m.Lock()
	defer ts.m.Unlock()
	ts.stopCnt++
}
