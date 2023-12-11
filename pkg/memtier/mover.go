package memtier

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MoverConfig represents the configuration for memory movement tasks.
type MoverConfig struct {
	// IntervalMs is the minimum interval between subsequent moves
	// in milliseconds
	IntervalMs int
	// Bandwidth is the maximum memory bandwidth in MB/s
	Bandwidth int
}

// moverDefaults represents the default configuration for memory movement tasks.
const moverDefaults string = "{\"IntervalMs\":10,\"Bandwidth\":100}"

// MoverTask represents a single memory movement task.
type MoverTask struct {
	pages  *Pages
	to     []Node
	offset int // index to the first page that is still to be moved
}

// Mover represents the memory mover, which manages and executes memory movement tasks.
type Mover struct {
	mutex     sync.Mutex
	running   bool
	tasks     []*MoverTask
	config    *MoverConfig
	ths       MoverTaskHandlerStatus
	waitCount uint64
	waitChan  map[uint64]chan MoverTaskHandlerStatus
	thsWaits  map[MoverTaskHandlerStatus][]uint64
	// channel for new tasks and (re)configuring
	toTaskHandler chan taskHandlerCmd
}

// taskHandlerCmd represents commands for the task handler.
type taskHandlerCmd int

// MoverTaskHandlerStatus represents the status of the task handler.
type MoverTaskHandlerStatus int

// taskStatus represents the status of a memory movement task.
type taskStatus int

const (
	thContinue taskHandlerCmd = iota
	thQuit
	thPause

	thsNotRunning MoverTaskHandlerStatus = iota
	thsPaused
	thsBusy
	thsAllDone

	tsContinue taskStatus = iota
	tsDone
	tsNoPagesOnSources
	tsNoDestinations
	tsBlocked
	tsError
)

// NewMover creates a new instance of the Mover.
func NewMover() *Mover {
	return &Mover{
		toTaskHandler: make(chan taskHandlerCmd, 8),
		waitChan:      make(map[uint64]chan MoverTaskHandlerStatus),
		thsWaits:      make(map[MoverTaskHandlerStatus][]uint64),
	}
}

// NewMoverTask creates a new memory movement task.
func NewMoverTask(pages *Pages, toNode Node) *MoverTask {
	return &MoverTask{
		pages: pages,
		to:    []Node{toNode},
	}
}

// String returns a string representation of the MoverTaskHandlerStatus
func (ths MoverTaskHandlerStatus) String() string {
	return map[MoverTaskHandlerStatus]string{
		thsNotRunning: "NotRunning",
		thsPaused:     "Paused",
		thsBusy:       "Busy",
		thsAllDone:    "AllDone",
	}[ths]
}

// String returns a string representation of the MoverTask.
func (mt *MoverTask) String() string {
	pid := mt.pages.Pid()
	p := mt.pages.Pages()
	nextAddr := "N/A"
	if len(p) > mt.offset {
		nextAddr = fmt.Sprintf("%x", p[mt.offset].Addr())
	}
	return fmt.Sprintf("MoverTask{pid: %d, next: %s, page: %d out of %d, dest: %v}", pid, nextAddr, mt.offset, len(p), mt.to)
}

// SetConfigJSON sets the configuration of the Mover from a JSON string.
func (m *Mover) SetConfigJSON(configJSON string) error {
	var config MoverConfig
	if err := unmarshal(configJSON, &config); err != nil {
		return err
	}
	return m.SetConfig(&config)
}

// SetConfig sets the configuration of the Mover.
func (m *Mover) SetConfig(config *MoverConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.config = config
	return nil
}

// GetConfigJSON gets the current configuration of the Mover as a JSON string.
func (m *Mover) GetConfigJSON() string {
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

// Start starts the Mover with the specified configuration.
func (m *Mover) Start() error {
	m.mutex.Lock()
	if m.config == nil {
		m.mutex.Unlock()
		if err := m.SetConfigJSON(moverDefaults); err != nil {
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

// Stop stops the Mover.
func (m *Mover) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.running {
		m.running = false
		m.toTaskHandler <- thQuit
	}
}

// Pause pauses the Mover.
func (m *Mover) Pause() {
	m.toTaskHandler <- thPause
}

// Continue resumes the Mover.
func (m *Mover) Continue() {
	m.toTaskHandler <- thContinue
}

// Tasks returns a copy of the current list of memory movement tasks.
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

// AddTask adds a new memory movement task to the Mover.
func (m *Mover) AddTask(task *MoverTask) {
	stats.Store(StatsHeartbeat{"mover.AddTask"})
	m.mutex.Lock()
	m.tasks = append(m.tasks, task)
	m.mutex.Unlock()
	m.toTaskHandler <- thContinue
}

// RemoveTask removes a memory movement task from the Mover.
func (m *Mover) RemoveTask(taskID int) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if taskID < 0 || taskID >= len(m.tasks) {
		return
	}
	m.tasks = append(m.tasks[0:taskID], m.tasks[taskID+1:]...)
}

// taskHandler is a goroutine that handles memory movement tasks.
func (m *Mover) taskHandler() {
	log.Debugf("Mover: online\n")
	defer func() {
		m.setTaskHandlerStatus(thsNotRunning)
		m.mutex.Lock()
		defer m.mutex.Unlock()
		log.Debugf("Mover: offline\n")
	}()
	m.setTaskHandlerStatus(thsPaused)
	for {
		// blocking channel read when there are no tasks
		cmd := <-m.toTaskHandler
		switch cmd {
		case thQuit:
			return
		case thPause:
			continue
		}
		m.setTaskHandlerStatus(thsBusy)
	busyloop:
		for {
			stats.Store(StatsHeartbeat{"mover.taskHandler"})
			// handle tasks, get all moving related attributes with single lock
			task, intervalMs, bandwidth := m.popTask()
			if task == nil {
				// no more tasks, back to blocking reads
				m.setTaskHandlerStatus(thsAllDone)
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
					m.setTaskHandlerStatus(thsPaused)
					break busyloop
				}
			default:
				time.Sleep(time.Duration(intervalMs) * time.Millisecond)
			}
		}
	}
}

// handleTask processes a memory movement task and returns its status.
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
	case NodeSwap:
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

// setTaskHandlerStatus changes current task handler status and
// informs those who were registered to wait new status.
func (m *Mover) setTaskHandlerStatus(ths MoverTaskHandlerStatus) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.ths = ths
	for _, waitId := range m.thsWaits[ths] {
		if c, ok := m.waitChan[waitId]; ok {
			c <- ths
			delete(m.waitChan, waitId)
			close(c)
		}
	}
	delete(m.thsWaits, ths)
}

// Wait(waitForStatus0, ...) returns a channel from which mover's task handler
// status change can be read. Only listed statuses are reported through the
// channel, and the channel gets closed immediately after reporting the first
// status change.
func (m *Mover) Wait(thss ...MoverTaskHandlerStatus) <-chan MoverTaskHandlerStatus {
	if len(thss) == 0 {
		return nil
	}
	c := make(chan MoverTaskHandlerStatus)
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for _, ths := range thss {
		if m.ths == ths {
			// The status is already what is waited for.
			go func() { c <- m.ths }()
			return c
		}
	}
	waitId := m.waitCount
	m.waitCount++
	m.waitChan[waitId] = c
	for _, ths := range thss {
		m.thsWaits[ths] = append(m.thsWaits[ths], waitId)
	}
	return c
}

// TaskCount returns the number of memory movement tasks in the Mover.
func (m *Mover) TaskCount() int {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return len(m.tasks)
}

// popTask pops a memory movement task from the task list.
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
