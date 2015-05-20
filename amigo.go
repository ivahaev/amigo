package amigo

import (
	"errors"
	log "github.com/ivahaev/go-logger"
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
	mutex          *sync.RWMutex
	handlerMutex   *sync.RWMutex
}

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

// Execute Actions in Asterisk. Returns immediately response from asterisk. Full response will follow.
// Usage amigo.Action(action map[string]string)
func (a *Amigo) Action(action M) (M, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.Connected() {
		result := a.ami.Exec(action)
		return result, nil
	}
	return nil, errors.New("Not connected to Asterisk")
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
			if a.defaultHandler != nil {
				a.defaultHandler(e)
			}
			if e["Event"] != "" && a.handlers[e["Event"]] != nil {
				a.handlers[e["Event"]](e)
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
	a.handlerMutex.Lock()
	defer a.handlerMutex.Unlock()
	if a.handlers[event] == nil {
		return errors.New("Handler not registered")
	}
	a.handlers[event] = nil
	return nil
}
