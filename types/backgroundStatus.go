package types

import (
	"fmt"
	"time"
)

type TaskID uint32

type BackgroundStatus struct {
	Name     string
	Message  string
	Progress int
	Total    int
	Status   TaskStatus
}

func (b *BackgroundStatus) OnProgress(current, total int, err error) {
	b.Progress = current
	b.Total = total
	b.Status = TaskRunning
	if err != nil {
		b.Message = fmt.Sprintf("Error: %v", err)
	}
}

func (b *BackgroundStatus) OnComplete(success, failed, total int) {
	b.Progress = success + failed
	b.Total = total
	b.Status = TaskFinished
	b.Message = fmt.Sprintf("finished: %v success %v failed %v total", success, failed, total)
	go func() {
		time.Sleep(8 * time.Second)
		b.Status = TaskCancelled
	}()
}

func (b *BackgroundStatus) OnCancel() {
	b.Status = TaskCancelled
}

type TaskMap map[TaskID]*BackgroundStatus
