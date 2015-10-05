package amigo

import (
	"errors"
	"github.com/ivahaev/amigo/uuid"
	log "github.com/ivahaev/go-logger"
	"strings"
	"sync"
	"time"
)

type M map[string]string

type handlerFunc func(M)

type Amigo struct {
	host           string
	port           string
	username       string
	secret         string
	ami            *amiAdapter
	events         chan M
	defaultChannel chan M
	defaultHandler handlerFunc
	handlers       map[string]handlerFunc
	capitalizeProps bool
	mutex          *sync.RWMutex
	handlerMutex   *sync.RWMutex
}

type agiCommand struct {
	c        chan M
	dateTime time.Time
}

// TODO: implement function to clear old data in handlers.
var (
	agiCommandsHandlers = make(map[string]agiCommand)
	agiCommandsMutex    = &sync.Mutex{}
)

// Usage: amigo.New(username string, secret string, [host string, [port string]])
func New(params ...string) *Amigo {
	var ami *amiAdapter
	var events chan M
	var host = "127.0.0.1"
	var port = "5038"
	var username, secret string
	if len(params) < 2 {
		panic("Wrong params for connect with Asterisk")
	}

	username = params[0]
	secret = params[1]
	if len(params) == 4 {
		host = params[2]
		port = params[3]
	} else if len(params) == 3 {
		host = params[2]
	}
	return &Amigo{
		host:         host,
		port:         port,
		username:     username,
		secret:       secret,
		ami:          ami,
		events:       events,
		handlers:     map[string]handlerFunc{},
		mutex:        &sync.RWMutex{},
		handlerMutex: &sync.RWMutex{},
	}
}

// If CapitalizeProps() calls with true, all prop's names will capitalized.
func (a *Amigo) CapitalizeProps(c bool) {
	a.capitalizeProps = c
}

// Execute Actions in Asterisk. Returns immediately response from asterisk. Full response will follow.
// Usage amigo.Action(action map[string]string)
func (a *Amigo) Action(action M) (M, error) {
	if a.Connected() {
		a.mutex.Lock()
		defer a.mutex.Unlock()
		result := a.ami.Exec(action)
		if a.capitalizeProps {
			e := M{}
			for k, v := range result {
				e[strings.ToUpper(k)] = v
			}
			return e, nil
		} else {
			return result, nil
		}
	}
	return nil, errors.New("Not connected to Asterisk")
}

// Execute Agi Actions in Asterisk. Returns full response.
// Usage amigo.AgiAction(channel, command string)
func (a *Amigo) AgiAction(channel, command string) (M, error) {
	if !a.Connected() {
		return nil, errors.New("Not connected to Asterisk")
	}
	commandId := uuid.NewV4()
	action := M{
		"Action":    "AGI",
		"Channel":   channel,
		"Command":   command,
		"CommandID": commandId,
	}

	ac := agiCommand{make(chan M), time.Now()}
	agiCommandsMutex.Lock()
	agiCommandsHandlers[commandId] = ac
	agiCommandsMutex.Unlock()

	a.mutex.Lock()
	result := a.ami.Exec(action)
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
	var err error
	for {
		am, err := newAMIAdapter(a.host, a.port)
		if err != nil {
			log.Error("AMI Connect error", err.Error())
		} else {
			a.mutex.Lock()
			a.ami = am
			a.mutex.Unlock()
			break
		}
		time.Sleep(time.Second)
	}
	log.Info("Connected to Asterisk", a.host, a.port)

	events, err := a.ami.Login(a.username, a.secret)
	a.mutex.Lock()
	a.events = events
	a.mutex.Unlock()
	if err != nil {
		log.Error("Asterisk login error", err.Error())
		return
	}
	log.Info("Logged into Asterisk", a.host, a.port, a.username)

	go func() {
		for {
			var e = <-a.events

			a.handlerMutex.RLock()
			defer a.handlerMutex.RUnlock()
			if a.defaultChannel != nil {
				a.defaultChannel <- e
			}
			var event = strings.ToUpper(e["Event"])
			if event != "" && a.handlers[event] != nil {
				if a.capitalizeProps {
					ev := M{}
					for k, v := range e {
						ev[strings.ToUpper(k)] = v
					}
					go a.handlers[event](ev)
					if a.defaultHandler != nil {
						go a.defaultHandler(ev)
					}
				} else {
					if a.defaultHandler != nil {
						go a.defaultHandler(e)
					}
					go a.handlers[event](e)
				}
			}
			if event == "ASYNCAGI" {
				commandId, ok := e["CommandID"]
				if !ok {
					continue
				}
				agiCommandsMutex.Lock()
				ac, ok := agiCommandsHandlers[commandId]
				if ok {
					delete(agiCommandsHandlers, commandId)
					agiCommandsMutex.Unlock()
					ac.c <- e
				} else {
					agiCommandsMutex.Unlock()
				}
			}
		}
	}()
}

// Returns true if successfully connected and logged in Asterisk and false otherwise.
func (a *Amigo) Connected() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.ami != nil && a.ami.Connected()
}

func (a *Amigo) RegisterDefaultHandler(f handlerFunc) error {
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	if a.defaultHandler != nil {
		return errors.New("DefaultHandler already registered")
	}
	a.defaultHandler = f
	return nil
}

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

// Set channel for receiving all events
func (a *Amigo) SetEventChannel(c chan M) {
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	a.defaultChannel = c
}

func (a *Amigo) UnregisterDefaultHandler(f handlerFunc) error {
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	if a.defaultHandler == nil {
		return errors.New("DefaultHandler not registered")
	}
	a.defaultHandler = nil
	return nil
}

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
