## routinepool
```go
type RoutinePool struct {
    // unexported fields
}

// New creates a goroutine pool.
func New(corePoolSize, maxPoolSize, queueSize uint64, maxIdleTime time.Duration) *RoutinePool
// Schedule schedules a task with its panicHandler.
func (p *RoutinePool) Schedule(task func(), panicHandler func(any)) error
// ShutDown closes the pool.
func (p *RoutinePool) ShutDown()
```

When a task coming, a core goroutine will be created immediately to handle the task, if the number of goroutines is less than `corePoolSize`. The following tasks will be put in a task queue, if the task queue has free space. A temporary goroutine will handle the task who can't be put in the task queue. Core goroutines and temporary goroutines poll tasks from the task queue all the time, but when the idle time of a temporary goroutine more than `maxIdleTime`, the temporary goroutine will be destroyed.

A task can't be scheduled when:

1. the pool is shutting down
2. task queue is full & all goroutines are busy & the number of goroutines has achieved `maxPoolSize`

## Demo

```go
package main

import (
    "github.com/YuHover/routinepool"
    "log"
    "time"
)

func main() {
    var (
        corePoolSize uint64 = 5
        maxPoolSize  uint64 = 10
        queueSize    uint64 = 10
        maxIdleTime         = time.Second
    )
    pool := routinepool.New(corePoolSize, maxPoolSize, queueSize, maxIdleTime)

    task := func(k int) {
        time.Sleep(time.Second * 10)
        log.Printf("task%d done\n", k)
    }
    for i := 0; uint64(i) < queueSize+maxPoolSize; i++ {
        k := i
        if err := pool.Schedule(func() { task(k) }, nil); err != nil {
            log.Println(err) // can't reach up
        }
    }

    if err := pool.Schedule(func() {}, nil); err != nil {
        log.Println(err) // pool is full: pool rejects to schedule the task
    }

    pool.ShutDown()
    log.Println(pool.IsShuttingDown()) // true, pool is shutting down
    if err := pool.Schedule(func() {}, nil); err != nil {
        log.Println(err) // pool is shutting
    }

    log.Println(pool.IsTerminated()) // false, waiting for the tasks left in the pool
    time.Sleep(time.Minute)
    log.Println(pool.IsTerminated()) // true, tasks left in the pool are handled completely
}
```

