package memtier

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type MoverConfig struct {
	// IntervalMs is the minimum interval between subsequent moves
	// in milliseconds
	IntervalMs int
	// Bandwidth is the maximum memory bandwidth in MB/s
	Bandwidth int
}

const moverDefaults string = "{\"IntervalMs\":10,\"Bandwidth\":100}"

type MoverTask struct {
	pages  *Pages
	to     []Node
	offset int // index to the first page that is still to be moved
}

type Mover struct {
	mutex         sync.Mutex
	running       bool
	tasks         []*MoverTask
	config        *MoverConfig
	toTaskHandler chan taskHandlerCmd
	// channel for new tasks and (re)configuring
}

type taskHandlerCmd int

type taskStatus int

const (
	thContinue taskHandlerCmd = iota
	thQuit
	thPause

	tsContinue taskStatus = iota
	tsDone
	tsNoPagesOnSources
	tsNoDestinations
	tsBlocked
	tsError
)

func NewMover() *Mover {
	return &Mover{
		toTaskHandler: make(chan taskHandlerCmd, 8),
	}
}

func NewMoverTask(pages *Pages, toNode Node) *MoverTask {
	return &MoverTask{
		pages: pages,
		to:    []Node{toNode},
	}
}

func (mt *MoverTask) String() string {
	pid := mt.pages.Pid()
	p := mt.pages.Pages()
	nextAddr := "N/A"
	if len(p) > mt.offset {
		nextAddr = fmt.Sprintf("%x", p[mt.offset].Addr())
	}
	return fmt.Sprintf("MoverTask{pid: %d, next: %s, page: %d out of %d, dest: %v}", pid, nextAddr, mt.offset, len(p), mt.to)
}

func (m *Mover) SetConfigJson(configJson string) error {
	var config MoverConfig
	if err := unmarshal(configJson, &config); err != nil {
		return err
	}
	return m.SetConfig(&config)
}

func (m *Mover) SetConfig(config *MoverConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.config = config
	return nil
}

func (m *Mover) GetConfigJson() string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.config == nil {
		return ""
	}
	if configStr, err := json.Marshal(m.config); err == nil {
		return string(configStr)
	}
	return ""
}

func (m *Mover) Start() error {
	m.mutex.Lock()
	if m.config == nil {
		m.mutex.Unlock()
		if err := m.SetConfigJson(moverDefaults); err != nil {
			return fmt.Errorf("start failed on default configuration error: %w", err)
		}
		m.mutex.Lock()
	}
	if !m.running {
		m.running = true
		go m.taskHandler()
	}
	m.mutex.Unlock()
	return nil
}

func (m *Mover) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.running {
		m.running = false
		m.toTaskHandler <- thQuit
	}
}

func (m *Mover) Pause() {
	m.toTaskHandler <- thPause
}

func (m *Mover) Continue() {
	m.toTaskHandler <- thContinue
}

func (m *Mover) Tasks() []*MoverTask {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	tasks := make([]*MoverTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		taskCopy := *task
		tasks = append(tasks, &taskCopy)
	}
	return tasks
}

func (m *Mover) AddTask(task *MoverTask) {
	stats.Store(StatsHeartbeat{"mover.AddTask"})
	m.mutex.Lock()
	m.tasks = append(m.tasks, task)
	m.mutex.Unlock()
	m.toTaskHandler <- thContinue
}

func (m *Mover) RemoveTask(taskId int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if taskId < 0 || taskId >= len(m.tasks) {
		return
	}
	m.tasks = append(m.tasks[0:taskId], m.tasks[taskId+1:]...)
}

func (m *Mover) taskHandler() {
	log.Debugf("Mover: online\n")
	defer func() {
		m.mutex.Lock()
		defer m.mutex.Unlock()
		log.Debugf("Mover: offline\n")
	}()
	for {
		// blocking channel read when there are no tasks
		cmd := <-m.toTaskHandler
		switch cmd {
		case thQuit:
			return
		case thPause:
			break
		}
	busyloop:
		for {
			stats.Store(StatsHeartbeat{"mover.taskHandler"})
			// handle tasks, get all moving related attributes with single lock
			task, intervalMs, bandwidth := m.popTask()
			if task == nil {
				// no more tasks, back to blocking reads
				break
			}
			if ts := m.handleTask(task, intervalMs, bandwidth); ts == tsContinue {
				m.mutex.Lock()
				m.tasks = append(m.tasks, task)
				m.mutex.Unlock()
			}
			// non-blocking channel read when there are tasks
			select {
			case cmd := <-m.toTaskHandler:
				switch cmd {
				case thQuit:
					return
				case thPause:
					break busyloop
				}
			default:
				time.Sleep(time.Duration(intervalMs) * time.Millisecond)
			}
		}
	}
}

func (m *Mover) handleTask(task *MoverTask, intervalMs, bandwidth int) taskStatus {
	pp := task.pages
	if task.offset > 0 {
		pp = pp.Offset(task.offset)
	}
	pageCountAfterOffset := len(pp.Pages())
	if pageCountAfterOffset == 0 {
		return tsDone
	}
	if task.to == nil || len(task.to) == 0 {
		return tsNoDestinations
	}
	// select destination memory node, now go with the first one
	toNode := task.to[0]
	// bandwidth is MB/s => bandwidth * 1024 is kB/s
	// constPagesize is 4096 kB/page
	// count is ([kB/s] / [kB/page] = [page/s]) * ([ms] / 1000 [ms/s] == [s]) = [page]
	count := (bandwidth * 1024 * 1024 / int(constPagesize)) * intervalMs / 1000
	if count == 0 {
		return tsBlocked
	}
	switch toNode {
	case NODE_SWAP:
		if err := pp.SwapOut(count); err != nil {
			return tsError
		}
	default:
		if _, err := pp.MoveTo(toNode, count); err != nil {
			return tsError
		}
	}
	task.offset += count
	if len(task.pages.Offset(count).Pages()) > 0 {
		return tsContinue
	}
	return tsDone
}

func (m *Mover) TaskCount() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return len(m.tasks)
}

func (m *Mover) popTask() (*MoverTask, int, int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.config == nil {
		return nil, 0, 0
	}
	taskCount := len(m.tasks)
	if taskCount == 0 {
		return nil, 0, 0
	}
	task := m.tasks[taskCount-1]
	m.tasks = m.tasks[:taskCount-1]
	return task, m.config.IntervalMs, m.config.Bandwidth
}
