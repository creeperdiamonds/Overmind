package world

import (
	"errors"
	"runtime/debug"
)

// Overmind fork: a world's transaction goroutine must never die silently.
//
// Upstream, a transaction that called runtime.Goexit (t.FailNow or a
// testify require from a test's world callback does) ended the goroutine
// that runs every transaction of the world: its waiters were never woken, the
// world stopped ticking, and Server.Close hung for ever on the players in it,
// with nothing logged. A panic outside a task's recovery took the process down
// without saying which transaction did it.
//
// Now handleTransactions runs each transaction through runGuarded. A panic is
// logged with its stack and re-raised, so a bug still stops the process as it
// did. A Goexit cannot be stopped, so it is logged with its stack, the
// transaction's waiters are released (abortTransaction) and a new goroutine
// takes over the queue.

// ErrTransactionExited is the error of a task whose callback called
// runtime.Goexit, which ends a goroutine without returning.
var ErrTransactionExited = errors.New("world: a transaction called runtime.Goexit")

// runGuarded runs tx, reporting whether it returned normally. It does not
// return when tx panics or calls runtime.Goexit.
func (w *World) runGuarded(tx transaction) {
	returned := false
	defer func() {
		if returned {
			return
		}
		if r := recover(); r != nil {
			w.conf.Log.Error("world transaction panicked", "panic", r, "stack", string(debug.Stack()))
			panic(r)
		}
		w.conf.Log.Error("world transaction called runtime.Goexit (t.FailNow in a world callback?); its goroutine is replaced and the world keeps running", "stack", string(debug.Stack()))
		abortTransaction(tx)
		go w.handleTransactions()
	}()
	tx.Run(w)
	returned = true
}

// abortTransaction releases whoever waits on a transaction that will never
// finish.
func abortTransaction(tx transaction) {
	switch t := tx.(type) {
	case normalTransaction:
		close(t.c)
	case weakTransaction:
		// It ran, in part: the entity's execWorld must stop waiting on it.
		t.cond.L.Lock()
		t.c <- true
		t.cond.Broadcast()
		t.cond.L.Unlock()
	case scheduledTransaction:
		t.task.abortRunning(ErrTransactionExited)
	}
}

// abortRunning finishes a task whose callback started but will never return.
// A task that finished already is left as it is.
func (t *Task) abortRunning(err error) {
	if t == nil || !t.state.CompareAndSwap(taskRunning, taskDone) {
		return
	}
	t.setErr(err)
	close(t.done)
}
