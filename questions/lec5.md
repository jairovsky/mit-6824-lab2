### Consider the following code from the "incorrect synchronization" examples: 
```go
var a string
var done bool

func setup() {
	a = "hello, world"
	done = true
}

func main() {
	go setup()
	for !done {
	}
	print(a)
}

```

#### Using the synchronization mechanisms of your choice, fix this code so it is guaranteed to have the intended behavior according to the Go language specification. Explain why your modification works in terms of the happens-before relation.

data-race-free solution:

```go
var a string
var done bool

func setup(done chan int) {
	a = "hello, world"
    done <- 0
}

func main() {
    done := make(chan int)
	go setup(done)
	<-done
	print(a)
}

```

following Go's memory model: 

- `a = "hello, world"` is guaranteed to complete before a value is sent to the `done` channel.
- the operation of sending a value to a channel is synchronized before the act of reading such value from the channel.
