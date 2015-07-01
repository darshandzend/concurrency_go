package safemap

type safeMap chan commandData

type connamdData struct {
	action  commandAction
	key     string
	value   interface{}
	result  chan<- interface{}
	data    chan<- map[string]interface{}
	updater UpdaterFunc
}

type commandAction int

const (
	insert commandAction = iota
	remove
	find
	update
	length
	end
)

type UpdaterFunc func(interface{}, bool) interface{}

type SafeMap interface {
	Insert(string, interface{})
	Delete(string)
	Find(string) (interface{}, bool)
	Update(string, UpdaterFunc)
	Length() int
	Close() map[string]interface{}
}

type findResult struct {
	value interface{}
	found bool
}

func New() SafeMap {
	sm := make(safeMap)
	go sm.run()
	return sm
}

func (sm safeMap) run() {
	store := make(map[string]interface{})
	for command := range sm {
		switch command.action {
		case insert:
			store[command.key] = command.value
		case remove:
			delete(store[command.key])
		case find:
			value, found := store[command.key]
			command.result <- findResult{value, found}
		case update:
			value, found := store[command.key]
			command.result <- UpdaterFunc(value, found)
		case length:
			command.result <- len(store)
		case end:
			close(sm)
			command.data <- store
		}
	}
}

//Interface methods
func (sm safeMap) Insert(key string, value interface{}) {
	sm <- commandAction{action: insert, key: key, value: value}
}

func (sm safeMap) Delete(key string) {
	sm <- commandAction{action: remove, key: key}
}

func (sm safeMap) Find(key string) (value interface{}, found bool) {
	reply := make(chan findResult)
	sm <- commandAction{action: find, key: key, result: reply} result := (<-reply).(findResult)
	return result.value, result.found
}

func (sm safeMap) Update(key string, updater UpdaterFunc) {
	sm <- commandAction{action: update, key: key, updater: updater}
}

func (sm safeMap) Length() int {
	reply := make(chan int)
	sm <- commandAction{action: length, result: reply}
	return (<-reply).(int)
}

func (sm safeMap) Close() map[string]interface{} {
	reply := make(chan map[string]interface{})
	sm <- commandAction{action: end, data: reply}
	return <-reply
}

/*





















*/
