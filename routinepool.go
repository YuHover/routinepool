package routinepool

import (
    "log"
    "sync"
    "sync/atomic"
    "time"
)

type poolStatus int32

const (
    running poolStatus = iota
    shutting
)

type RoutinePool struct {
    corePool    chan struct{}
    tempPool    chan struct{}
    queue       chan poolTask
    maxIdleTime time.Duration
    rwmu        sync.RWMutex // synchronize between Schedule and ShutDown
    shutOnce    sync.Once
    status      poolStatus
    active      uint64
}

type poolTask struct {
    task         func()
    panicHandler func(any)
}

type PoolRejectsScheduleErr struct {
    reason string
}

func (e *PoolRejectsScheduleErr) Error() string {
    return "pool rejects to schedule the task: " + e.reason
}

// New creates a goroutine pool.
// When a task coming, a core goroutine will be created immediately to handle the task,
// if the number of goroutines is less than corePoolSize.
// The following tasks will be put in a task queue, if the task queue has free space.
// A temporary goroutine will handle the task who can't be put in the task queue.
// Core goroutines and temporary goroutines poll tasks from the task queue all the time,
// but when the idle time of a temporary goroutine more than maxIdleTime, the temporary goroutine will be destroyed.
// When 1.task queue is full 2.all goroutines are busy 3.the number of goroutines has achieved maxPoolSize
// 4.the pool is shutting down, a task can't be scheduled.
func New(corePoolSize, maxPoolSize, queueSize uint64, maxIdleTime time.Duration) *RoutinePool {
    if maxPoolSize == 0 {
        panic("maxPoolSize should be greater than 0")
    }

    if maxPoolSize < corePoolSize {
        panic("maxPoolSize should be greater than or equal to corePoolSize")
    }

    return &RoutinePool{
        corePool:    make(chan struct{}, corePoolSize),
        tempPool:    make(chan struct{}, maxPoolSize-corePoolSize),
        queue:       make(chan poolTask, queueSize),
        maxIdleTime: maxIdleTime,
        status:      running,
        active:      0,
    }
}

func (p *RoutinePool) Schedule(task func(), panicHandler func(any)) error {
    if task == nil {
        return &PoolRejectsScheduleErr{reason: "task is nil"}
    }

    if p.getPoolStatus() != running {
        return &PoolRejectsScheduleErr{reason: "pool is not running"}
    }

    p.rwmu.RLock()
    defer p.rwmu.RUnlock()

    if p.status != running {
        return &PoolRejectsScheduleErr{reason: "pool is not running"}
    }

    t := poolTask{
        task:         task,
        panicHandler: panicHandler,
    }

    select {
    case p.corePool <- struct{}{}:
        go p.work(t)
        return nil
    default:
    }

    select {
    case p.queue <- t:
        return nil
    default:
    }

    select {
    case p.tempPool <- struct{}{}:
        go p.workWithExpiration(t)
        return nil
    default:
    }

    return &PoolRejectsScheduleErr{reason: "pool was full just now"}
}

func (p *RoutinePool) ShutDown() {
    p.shutOnce.Do(func() {
        p.rwmu.Lock()
        defer p.rwmu.Unlock()

        p.setPoolStatus(shutting)
        close(p.corePool)
        close(p.tempPool)
        close(p.queue)
    })
}

func (p *RoutinePool) work(t poolTask) {
    p.incRunningRoutine()
    defer p.decRunningRoutine()

    defer func() {
        if err := recover(); err != nil {
            if t.panicHandler != nil {
                t.panicHandler(err)
            } else {
                log.Printf("recover panic: %v", err)
            }

            if p.getPoolStatus() != shutting {
                go p.work(poolTask{task: func() {}}) // core goroutine panic, restart one
            }
        }
    }()

    t.task()
    for t := range p.queue {
        t.task()
    } // ShutDown was called and the tasks in the queue are handled completely
}

func (p *RoutinePool) workWithExpiration(t poolTask) {
    p.incRunningRoutine()
    defer p.decRunningRoutine()

    defer func() { <-p.tempPool }()

    defer func() {
        if err := recover(); err != nil {
            if t.panicHandler != nil {
                t.panicHandler(err)
            } else {
                log.Printf("recover panic: %v", err)
            }
        }
    }()

    t.task()
    for {
        select {
        case <-time.After(p.maxIdleTime):
            return
        case t, ok := <-p.queue:
            if !ok {
                return // ShutDown was called and the tasks in the queue are handled completely
            }
            t.task()
        }
    }
}

func (p *RoutinePool) IsShuttingDown() bool {
    return p.getPoolStatus() == shutting
}

func (p *RoutinePool) IsTerminated() bool {
    return p.getPoolStatus() == shutting && atomic.LoadUint64(&p.active) == 0
}

func (p *RoutinePool) incRunningRoutine() {
    atomic.AddUint64(&p.active, 1)
}

func (p *RoutinePool) decRunningRoutine() {
    atomic.AddUint64(&p.active, ^uint64(0))
}

func (p *RoutinePool) getPoolStatus() poolStatus {
    return poolStatus(atomic.LoadInt32((*int32)(&p.status)))
}

func (p *RoutinePool) setPoolStatus(s poolStatus) {
    atomic.StoreInt32((*int32)(&p.status), int32(s))
}
