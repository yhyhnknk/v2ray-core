package task

import (
	"context"

	"v2ray.com/core/common/signal/semaphore"
)

type Task func() error

type executionContext struct {
	ctx       context.Context
	tasks     []Task
	onSuccess Task
	onFailure Task
}

func (c *executionContext) executeTask() error {
	if len(c.tasks) == 0 {
		return nil
	}

	// Reuse current goroutine if we only have one task to run.
	if len(c.tasks) == 1 && c.ctx == nil {
		return c.tasks[0]()
	}

	ctx := context.Background()

	if c.ctx != nil {
		ctx = c.ctx
	}

	return executeParallel(ctx, c.tasks)
}

func (c *executionContext) run() error {
	err := c.executeTask()
	if err == nil && c.onSuccess != nil {
		return c.onSuccess()
	}
	if err != nil && c.onFailure != nil {
		return c.onFailure()
	}
	return err
}

type ExecutionOption func(*executionContext)

func WithContext(ctx context.Context) ExecutionOption {
	return func(c *executionContext) {
		c.ctx = ctx
	}
}

func Parallel(tasks ...Task) ExecutionOption {
	return func(c *executionContext) {
		c.tasks = append(c.tasks, tasks...)
	}
}

// Sequential runs all tasks sequentially, and returns the first error encountered.Sequential
// Once a task returns an error, the following tasks will not run.
func Sequential(tasks ...Task) ExecutionOption {
	return func(c *executionContext) {
		switch len(tasks) {
		case 0:
			return
		case 1:
			c.tasks = append(c.tasks, tasks[0])
		default:
			c.tasks = append(c.tasks, func() error {
				return execute(tasks...)
			})
		}
	}
}

func OnSuccess(task Task) ExecutionOption {
	return func(c *executionContext) {
		c.onSuccess = task
	}
}

func OnFailure(task Task) ExecutionOption {
	return func(c *executionContext) {
		c.onFailure = task
	}
}

func Single(task Task, opts ...ExecutionOption) Task {
	return Run(append([]ExecutionOption{Sequential(task)}, opts...)...)
}

func Run(opts ...ExecutionOption) Task {
	var c executionContext
	for _, opt := range opts {
		opt(&c)
	}
	return func() error {
		return c.run()
	}
}

// execute runs a list of tasks sequentially, returns the first error encountered or nil if all tasks pass.
func execute(tasks ...Task) error {
	for _, task := range tasks {
		if err := task(); err != nil {
			return err
		}
	}
	return nil
}

// executeParallel executes a list of tasks asynchronously, returns the first error encountered or nil if all tasks pass.
func executeParallel(ctx context.Context, tasks []Task) error {
	n := len(tasks)
	s := semaphore.New(n)
	done := make(chan error, 1)

	for _, task := range tasks {
		<-s.Wait()
		go func(f Task) {
			err := f()
			if err == nil {
				s.Signal()
				return
			}

			select {
			case done <- err:
			default:
			}
		}(task)
	}

	for i := 0; i < n; i++ {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-s.Wait():
		}
	}

	return nil
}
