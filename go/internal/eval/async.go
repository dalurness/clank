package eval

import (
	"github.com/dalurness/clank/internal/token"
)

// ── Async task types ──

type AsyncTaskStatus int

const (
	TaskPending   AsyncTaskStatus = iota
	TaskCompleted
	TaskFailed
	TaskCancelled
)

type AsyncTask struct {
	ID         int
	Status     AsyncTaskStatus
	Result     Value
	Error      string
	Body       func() Value
	CancelFlag bool
	ShieldDepth int
	GroupID    int
}

type AsyncTaskGroup struct {
	ID       int
	Children []*AsyncTask
}

// ── Channel type ──

type EvalChannel struct {
	ID           int
	Buffer       []Value
	Capacity     int
	SenderOpen   bool
	ReceiverOpen bool
}

// ── Global async state ──

var (
	nextAsyncTaskID  int
	nextAsyncGroupID int
	nextChannelID    int
	activeGroupStack []*AsyncTaskGroup
	currentTask      *AsyncTask
)

// resetAsyncState clears all async state for a fresh run.
func resetAsyncState() {
	nextAsyncTaskID = 1
	nextAsyncGroupID = 1
	nextChannelID = 1
	activeGroupStack = nil
	currentTask = nil
}

// ── Async task execution ──

func runAsyncTask(task *AsyncTask, loc token.Loc) {
	savedTask := currentTask
	currentTask = task
	defer func() {
		if r := recover(); r != nil {
			if re, ok := r.(*RuntimeError); ok && re.Code == "E011" {
				task.Status = TaskCancelled
			} else if re, ok := r.(*RuntimeError); ok {
				task.Status = TaskFailed
				task.Error = re.Message
			} else {
				currentTask = savedTask
				panic(r)
			}
		}
		currentTask = savedTask
	}()

	if task.CancelFlag && task.ShieldDepth == 0 {
		task.Status = TaskCancelled
		return
	}
	result := task.Body()
	task.Result = result
	task.Status = TaskCompleted
}

// ── Async builtins ──

func AsyncBuiltins() map[string]func([]Value, token.Loc) Value {
	return map[string]func([]Value, token.Loc) Value{
		"spawn": func(args []Value, loc token.Loc) Value {
			body := args[0]
			if _, ok := body.(ValClosure); !ok {
				if _, ok := body.(ValBuiltin); !ok {
					panic(runtimeError("E204", "spawn expects a function", loc))
				}
			}
			taskID := nextAsyncTaskID
			nextAsyncTaskID++
			var group *AsyncTaskGroup
			if len(activeGroupStack) > 0 {
				group = activeGroupStack[len(activeGroupStack)-1]
			}
			task := &AsyncTask{
				ID:     taskID,
				Status: TaskPending,
				Body:   func() Value { return ApplyValue(body, []Value{}, loc) },
			}
			if group != nil {
				task.GroupID = group.ID
				group.Children = append(group.Children, task)
			}
			return ValFuture{Task: task}
		},

		"await": func(args []Value, loc token.Loc) Value {
			futVal, ok := args[0].(ValFuture)
			if !ok {
				panic(runtimeError("E200", "await expects a Future", loc))
			}
			task := futVal.Task
			// Check cancellation of current task
			if currentTask != nil && currentTask.CancelFlag && currentTask.ShieldDepth == 0 {
				currentTask.Status = TaskCancelled
				panic(runtimeError("E011", "task cancelled", loc))
			}
			// Run the task if pending
			if task.Status == TaskPending {
				runAsyncTask(task, loc)
			}
			switch task.Status {
			case TaskCompleted:
				if task.Result != nil {
					return task.Result
				}
				return ValUnit{}
			case TaskFailed:
				msg := task.Error
				if msg == "" {
					msg = "task failed"
				}
				panic(runtimeError("E014", msg, loc))
			case TaskCancelled:
				panic(runtimeError("E011", "awaited task was cancelled", loc))
			}
			return ValUnit{}
		},

		"task-group": func(args []Value, loc token.Loc) Value {
			body := args[0]
			if _, ok := body.(ValClosure); !ok {
				if _, ok := body.(ValBuiltin); !ok {
					panic(runtimeError("E204", "task-group expects a function", loc))
				}
			}
			group := &AsyncTaskGroup{
				ID: nextAsyncGroupID,
			}
			nextAsyncGroupID++
			activeGroupStack = append(activeGroupStack, group)

			var result Value
			var bodyErr interface{}
			func() {
				defer func() {
					if r := recover(); r != nil {
						bodyErr = r
					}
				}()
				result = ApplyValue(body, []Value{}, loc)
			}()

			activeGroupStack = activeGroupStack[:len(activeGroupStack)-1]

			// Cancel still-pending children
			for _, child := range group.Children {
				if child.Status == TaskPending {
					child.CancelFlag = true
				}
			}
			// Run remaining children (they'll observe cancellation)
			for _, child := range group.Children {
				if child.Status == TaskPending {
					runAsyncTask(child, loc)
				}
			}
			// Check for child failures
			var firstChildErr *RuntimeError
			for _, child := range group.Children {
				if child.Status == TaskFailed && firstChildErr == nil {
					msg := child.Error
					if msg == "" {
						msg = "child task failed"
					}
					firstChildErr = runtimeError("E014", msg, loc)
				}
			}

			if bodyErr != nil {
				panic(bodyErr)
			}
			if firstChildErr != nil {
				panic(firstChildErr)
			}
			return result
		},

		"task-yield": func(_ []Value, loc token.Loc) Value {
			if currentTask != nil && currentTask.CancelFlag && currentTask.ShieldDepth == 0 {
				currentTask.Status = TaskCancelled
				panic(runtimeError("E011", "task cancelled", loc))
			}
			return ValUnit{}
		},

		"sleep": func(_ []Value, loc token.Loc) Value {
			if currentTask != nil && currentTask.CancelFlag && currentTask.ShieldDepth == 0 {
				currentTask.Status = TaskCancelled
				panic(runtimeError("E011", "task cancelled", loc))
			}
			return ValUnit{}
		},

		"is-cancelled": func(_ []Value, _ token.Loc) Value {
			cancelled := currentTask != nil && currentTask.CancelFlag && currentTask.ShieldDepth == 0
			return ValBool{Val: cancelled}
		},

		"shield": func(args []Value, loc token.Loc) Value {
			body := args[0]
			if _, ok := body.(ValClosure); !ok {
				if _, ok := body.(ValBuiltin); !ok {
					panic(runtimeError("E204", "shield expects a function", loc))
				}
			}
			if currentTask != nil {
				currentTask.ShieldDepth++
			}
			var result Value
			func() {
				defer func() {
					if currentTask != nil {
						currentTask.ShieldDepth--
					}
				}()
				result = ApplyValue(body, []Value{}, loc)
			}()
			return result
		},

		// Channel builtins
		"channel": func(args []Value, loc token.Loc) Value {
			cap, ok := args[0].(ValInt)
			if !ok {
				panic(runtimeError("E204", "channel expects an Int capacity", loc))
			}
			ch := &EvalChannel{
				ID:           nextChannelID,
				Buffer:       nil,
				Capacity:     int(cap.Val),
				SenderOpen:   true,
				ReceiverOpen: true,
			}
			nextChannelID++
			return ValTuple{Elements: []Value{
				ValSender{Channel: ch},
				ValReceiver{Channel: ch},
			}}
		},

		"send": func(args []Value, loc token.Loc) Value {
			sender, ok := args[0].(ValSender)
			if !ok {
				panic(runtimeError("E204", "send expects a Sender", loc))
			}
			ch := sender.Channel
			if !ch.ReceiverOpen {
				panic(runtimeError("E012", "send: channel receiver is closed", loc))
			}
			if !ch.SenderOpen {
				panic(runtimeError("E012", "send: sender is closed", loc))
			}
			ch.Buffer = append(ch.Buffer, args[1])
			return ValUnit{}
		},

		"recv": func(args []Value, loc token.Loc) Value {
			receiver, ok := args[0].(ValReceiver)
			if !ok {
				panic(runtimeError("E204", "recv expects a Receiver", loc))
			}
			ch := receiver.Channel
			if len(ch.Buffer) > 0 {
				val := ch.Buffer[0]
				ch.Buffer = ch.Buffer[1:]
				return val
			}
			if !ch.SenderOpen {
				panic(runtimeError("E012", "recv: channel is closed and empty", loc))
			}
			panic(runtimeError("E012", "recv: channel is empty", loc))
		},

		"try-recv": func(args []Value, loc token.Loc) Value {
			receiver, ok := args[0].(ValReceiver)
			if !ok {
				panic(runtimeError("E204", "try-recv expects a Receiver", loc))
			}
			ch := receiver.Channel
			if len(ch.Buffer) > 0 {
				val := ch.Buffer[0]
				ch.Buffer = ch.Buffer[1:]
				return ValVariant{Name: "Some", Fields: []Value{val}}
			}
			return ValVariant{Name: "None", Fields: []Value{}}
		},

		"close-sender": func(args []Value, loc token.Loc) Value {
			sender, ok := args[0].(ValSender)
			if !ok {
				panic(runtimeError("E204", "close-sender expects a Sender", loc))
			}
			sender.Channel.SenderOpen = false
			return ValUnit{}
		},

		"close-receiver": func(args []Value, loc token.Loc) Value {
			receiver, ok := args[0].(ValReceiver)
			if !ok {
				panic(runtimeError("E204", "close-receiver expects a Receiver", loc))
			}
			receiver.Channel.ReceiverOpen = false
			return ValUnit{}
		},
	}
}
