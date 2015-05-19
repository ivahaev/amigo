package amigo

import (
    "github.com/ivahaev/amigo/amiConnect"
    log "github.com/ivahaev/go-logger"
    "time"
    "errors"
)

var err error
var a *amiConnect.AMIAdapter
var events chan map[string]string
var host = "127.0.0.1"
var port = "5038"
var username, secret string

func Action(action map[string]string) (map[string]string, error) {
    if a != nil && a.Connected {
        result := a.Exec(action)
        return result, nil
    }
    return nil, errors.New("Not connected")
}

func Connect(c chan [string]string, params ...string) {
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
        port = "5038"
    } else {
        host = "127.0.0.1"
        port = "5038"
    }
    for {
        a, err = amiConnect.NewAMIAdapter(host, port)
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
            c = <-events
        }
    }()
}