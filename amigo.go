package amigo

import (
	"errors"
	"github.com/ivahaev/amigo/ami"
	log "github.com/ivahaev/go-logger"
	"time"
)

var a *ami.AMIAdapter
var events chan map[string]string

type Amigo struct {
	a *ami.AMIAdapter
	events chan map[string]string
	c chan map[string]string
}

func New (params ...string) *Amigo {
	var err error
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

	for {
		a, err = ami.NewAMIAdapter(host, port)
		if err != nil {
			log.Error("AMI Connect error", err.Error())
		} else {
			break
		}
		time.Sleep(time.Second)
	}
	log.Info("Connected to Asterisk", host, port)

	events, err = a.Login(username, secret)
	if err != nil {
		log.Error("Asterisk login error", err.Error())
	}
	log.Info("Logged into Asterisk", host, port, username)

	return &Amigo{a: a, events: events}
}

// Execute Actions in Asterisk. Returns immediately response from asterisk. Full response will follow.
// Usage amigo.Action(action map[string]string)
func Action(action map[string]string) (map[string]string, error) {
	if a != nil && a.Connected {
		result := a.Exec(action)
		return result, nil
	}
	return nil, errors.New("Not connected to Asterisk")
}

// Connect with Asterisk.
// Usage: amigo.Connect(eventChannel chan map[string]string, username string, secret string, [host string, [port string]])
// If connect fails, will try to reconnect every second.
func Connect(c chan map[string]string, params ...string) {
	log.Info("Connect command")
	var err error
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

	for {
		a, err = ami.NewAMIAdapter(host, port)
		if err != nil {
			log.Error("AMI Connect error", err.Error())
		} else {
			break
		}
		time.Sleep(time.Second)
	}
	log.Info("Connected to Asterisk", host, port)

	events, err = a.Login(username, secret)
	if err != nil {
		log.Error("Asterisk login error", err.Error())
	}
	log.Info("Logged into Asterisk", host, port, username)

	func() {
		for {
			var e = <-events
			c <- e
		}
	}()
}

// Returns true if successfully connected and logged in Asterisk and false otherwise.
func Connected() bool {
	return a != nil && a.Connected
}
