# godispatch
This is a general purpose object dispatcher for go.

It can be used to either asynchronously dispatch queues of objects or synchronously dispatch single objects to multiple handlers.

[![GoDoc](https://godoc.org/github.com/markus-wa/godispatch?status.svg)](https://godoc.org/github.com/markus-wa/godispatch)
[![Build Status](https://travis-ci.org/markus-wa/godispatch.svg?branch=master)](https://travis-ci.org/markus-wa/godispatch)
[![Go Report](https://goreportcard.com/badge/github.com/markus-wa/godispatch)](https://goreportcard.com/report/github.com/markus-wa/godispatch)
[![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat)](LICENSE.md)

## Go Get

	go get github.com/markus-wa/godispatch

## Example
```go
import (
	"fmt"
	dp "github.com/markus-wa/godispatch"
)

func main() {
	d := dp.Dispatcher{}
	// Register a handler for string (not *string!)
	// We get the string Type by calling Elem() on reflect.Type *string)
	// This is faster than doing reflect.TypeOf("")
	d.RegisterHandler(func(s string) {
		fmt.Println("Handled string", s)
	})
	d.RegisterHandler(func(obj interface{}) {
		fmt.Println("Handled object", obj)
	})

	d.Dispatch("Hello")
	// Prints (in this order - as the object handler was registered after the string handler)
	// "Handled string Hello"
	// "Handled object Hello"
	d.Dispatch(123)
	// Prints "Handled object 123"
}
```

## Queue Example
```go
type Event struct {
	reference int
	message   string
}

type TriggerEvent struct{}

func main() {
	d := dp.Dispatcher{}
	// If you wanted to handle pointers of the Event just remove .Elem(),
	// use *Event for the type assertion and send pointers
	d.RegisterHandler(func(e Event) {
		fmt.Println("Handled Event", e)
		// Handle event
	})
	d.RegisterHandler(func(te TriggerEvent) {
		// Do stuff when we receive a 'TriggerEvent'
	})

	// Buffered to improve performance by avoiding locking
	q := make(chan interface{}, 5)
	q2 := make(chan interface{}, 5)
	q3 := make(chan interface{}, 5)

	// Add queues to dispatcher
	d.AddQueues(q, q2, q3)

	// Send some events
	for i := 0; i < 10; i++ {
		q <- Event{i, "abc"}
		q <- TriggerEvent{}
		q2 <- Event{i, "def"}
		q3 <- Event{i, "geh"}
		// Events that are not in the same queue will be handled concurrently
		d.SyncQueues(q)
		// Do stuff that requires events in q (but not q2 & q3) to be handled
	}
	d.SyncAllQueues()
	// Do stuff that requires events of q, q2 & q3 to be handled

	// Maybe send some more events . . .
	q <- TriggerEvent{}

	// Remove queues q & q2
	d.RemoveQueues(q, q2)

	q3 <- Event{}

	// Also remove q3
	d.RemoveAllQueues()
}
```
