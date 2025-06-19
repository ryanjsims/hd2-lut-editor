package types

type TaskStatus uint32

const (
	TaskIdle      TaskStatus = 0
	TaskRunning   TaskStatus = 1
	TaskFailed    TaskStatus = 2
	TaskFinished  TaskStatus = 3
	TaskCancelled TaskStatus = 4
)
