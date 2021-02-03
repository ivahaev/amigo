package amigo

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ivahaev/amigo/uuid"
)

var (
	version = "0.1.9"

	// TODO: implement function to clear old data in handlers.
	agiCommandsHandlers = make(map[string]agiCommand)
	agiCommandsMutex    = &sync.Mutex{}
	errNotConnected     = errors.New("Not connected to Asterisk")
)

type handlerFunc func(map[string]string)
type eventHandlerFunc func(string)

// Amigo is a main package struct
type Amigo struct {
	settings        *Settings
	ami             *amiAdapter
	defaultChannel  chan map[string]string
	defaultHandler  handlerFunc
	handlers        map[string]handlerFunc
	eventHandlers   map[string][]eventHandlerFunc
	capitalizeProps bool
	connectCalled   bool
	mutex           *sync.RWMutex
	handlerMutex    *sync.RWMutex
}

// Settings represents connection settings for Amigo.
// Default:
// Username = admin,
// Password = amp111,
// Host = 127.0.0.1,
// Port = 5038,
// ActionTimeout = 3s
// DialTimeout = 10s
type Settings struct {
	Username          string
	Password          string
	Host              string
	Port              string
	ActionTimeout     time.Duration
	DialTimeout       time.Duration
	ReconnectInterval time.Duration
	Keepalive         bool
}

type agiCommand struct {
	c        chan map[string]string
	dateTime time.Time
}

// New creates new Amigo struct with credentials provided and returns pointer to it
// Usage: New(username string, secret string, [host string, [port string]])
func New(settings *Settings) *Amigo {
	prepareSettings(settings)

	var ami *amiAdapter
	return &Amigo{
		settings:      settings,
		ami:           ami,
		handlers:      map[string]handlerFunc{},
		eventHandlers: map[string][]eventHandlerFunc{},
		mutex:         &sync.RWMutex{},
		handlerMutex:  &sync.RWMutex{},
	}
}

// CapitalizeProps used to capitalise all prop's names when true provided.
func (a *Amigo) CapitalizeProps(c bool) {
	a.capitalizeProps = c
}

// Action used to execute Actions in Asterisk. Returns immediately response from asterisk. Full response will follow.
// Usage amigo.Action(action map[string]string)
func (a *Amigo) Action(action map[string]string) (map[string]string, error) {
	if !a.Connected() {
		return nil, errNotConnected
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()
	result := a.ami.exec(action)
	if a.capitalizeProps {
		e := map[string]string{}
		for k, v := range result {
			e[strings.ToUpper(k)] = v
		}
		return e, nil
	}

	if (strings.ToLower(action["Action"]) == "logoff") {
		a.ami.reconnect = false;
	}
	return result, nil
}

// AgiAction used to execute Agi Actions in Asterisk. Returns full response.
// Usage amigo.AgiAction(channel, command string)
func (a *Amigo) AgiAction(channel, command string) (map[string]string, error) {
	if !a.Connected() {
		return nil, errNotConnected
	}

	commandID := uuid.NewV4()
	action := map[string]string{
		"Action":    "AGI",
		"Channel":   channel,
		"Command":   command,
		"CommandID": commandID,
	}

	ac := agiCommand{make(chan map[string]string), time.Now()}
	agiCommandsMutex.Lock()
	agiCommandsHandlers[commandID] = ac
	agiCommandsMutex.Unlock()

	a.mutex.Lock()
	result := a.ami.exec(action)
	a.mutex.Unlock()
	if result["Response"] != "Success" {
		return result, errors.New("Fail with command")
	}
	result = <-ac.c
	delete(result, "CommandID")
	if a.capitalizeProps {
		for k, v := range result {
			result[strings.ToUpper(k)] = v
			delete(result, k)
		}
	}

	return result, nil
}

// Connect with Asterisk.
// If connect fails, will try to reconnect every second.
func (a *Amigo) Connect() {
	var connectCalled bool
	a.mutex.RLock()
	connectCalled = a.connectCalled
	a.mutex.RUnlock()
	if connectCalled {
		return
	}

	a.mutex.Lock()
	a.connectCalled = true
	a.mutex.Unlock()

	for {
		am, err := newAMIAdapter(a.settings, a.emitEvent)
		if err != nil {
			go a.emitEvent("error", fmt.Sprintf("AMI Connect error: %s", err.Error()))
		} else {
			a.mutex.Lock()
			a.ami = am
			a.mutex.Unlock()
			break
		}
		time.Sleep(time.Second)
	}

	go func() {
		for {
			var e = <-a.ami.eventsChan
			a.handlerMutex.RLock()

			if a.defaultChannel != nil {
				a.defaultChannel <- e
			}

			var event = strings.ToUpper(e["Event"])
			if len(event) != 0 && (a.handlers[event] != nil || a.defaultHandler != nil) {
				if a.capitalizeProps {
					ev := map[string]string{}
					for k, v := range e {
						ev[strings.ToUpper(k)] = v
					}

					if a.handlers[event] != nil {
						a.handlers[event](ev)
					}

					if a.defaultHandler != nil {
						a.defaultHandler(ev)
					}
				} else {
					if a.defaultHandler != nil {
						a.defaultHandler(e)
					}

					if a.handlers[event] != nil {
						a.handlers[event](e)
					}
				}
			}

			if event == "ASYNCAGI" {
				commandID, ok := e["CommandID"]
				if !ok {
					a.handlerMutex.RUnlock()
					continue
				}
				agiCommandsMutex.Lock()
				ac, ok := agiCommandsHandlers[commandID]
				if ok {
					delete(agiCommandsHandlers, commandID)
					agiCommandsMutex.Unlock()
					ac.c <- e
				} else {
					agiCommandsMutex.Unlock()
				}
			}

			a.handlerMutex.RUnlock()
		}
	}()
}

// Connected returns true if successfully connected and logged in Asterisk and false otherwise.
func (a *Amigo) Connected() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.ami != nil && a.ami.online()
}

// On register handler for package events. Now amigo will emit two types of events:
// "connect" fired on connection success and "error" on any error occured.
func (a *Amigo) On(event string, handler func(string)) {
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()

	if _, ok := a.eventHandlers[event]; !ok {
		a.eventHandlers[event] = []eventHandlerFunc{}
	}
	a.eventHandlers[event] = append(a.eventHandlers[event], handler)
}

// RegisterDefaultHandler registers handler function that will called on each event
func (a *Amigo) RegisterDefaultHandler(f handlerFunc) error {
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()

	if a.defaultHandler != nil {
		return errors.New("DefaultHandler already registered")
	}
	a.defaultHandler = f
	return nil
}

// RegisterHandler registers handler function for provided event name
func (a *Amigo) RegisterHandler(event string, f handlerFunc) error {
	event = strings.ToUpper(event)
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	if a.handlers[event] != nil {
		return errors.New("Handler already registered")
	}
	a.handlers[event] = f
	return nil
}

// SetEventChannel sets channel for receiving all events
func (a *Amigo) SetEventChannel(c chan map[string]string) {
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	a.defaultChannel = c
}

// UnregisterDefaultHandler removes default handler function
func (a *Amigo) UnregisterDefaultHandler(f handlerFunc) error {
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	if a.defaultHandler == nil {
		return errors.New("DefaultHandler not registered")
	}
	a.defaultHandler = nil
	return nil
}

// EventsChanLength returns the current size of eventsChan
func (a *Amigo) EventsChanLength() int {
	return len(a.ami.eventsChan)
}

// UnregisterHandler removes handler function for provided event name
func (a *Amigo) UnregisterHandler(event string, f handlerFunc) error {
	event = strings.ToUpper(event)
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	if a.handlers[event] == nil {
		return errors.New("Handler not registered")
	}
	a.handlers[event] = nil
	return nil
}

func (a *Amigo) emitEvent(name, message string) {
	a.handlerMutex.RLock()
	defer a.handlerMutex.RUnlock()

	if len(a.eventHandlers) == 0 {
		return
	}

	handlers, ok := a.eventHandlers[name]
	if !ok {
		return
	}

	for _, h := range handlers {
		h(message)
	}
}

func prepareSettings(settings *Settings) {
	if settings.Username == "" {
		settings.Username = "admin"
	}
	if settings.Password == "" {
		settings.Password = "amp111"
	}
	if settings.Host == "" {
		settings.Host = "127.0.0.1"
	}
	if settings.Port == "" {
		settings.Port = "5038"
	}
	if settings.ActionTimeout == 0 {
		settings.ActionTimeout = actionTimeout
	}
	if settings.DialTimeout == 0 {
		settings.DialTimeout = dialTimeout
	}
	if settings.ReconnectInterval == 0 {
		settings.ReconnectInterval = time.Second
	}
}
