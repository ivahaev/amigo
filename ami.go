package amigo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ivahaev/amigo/uuid"
)

var actionTimeout = time.Second * 3

type amiAdapter struct {
	EventsChan chan map[string]string

	ip       string
	port     string
	username string
	password string

	conn      *net.TCPConn
	connected bool

	actionsChan   chan map[string]string
	responseChans map[string]chan map[string]string
	readErrChan   chan error
	writeErrChan  chan error
	mutex         *sync.RWMutex
	emitEvent     func(string, string)
}

func newAMIAdapter(ip, port, username, password string, eventEmitter func(string, string)) (*amiAdapter, error) {
	var a = new(amiAdapter)
	a.ip = ip
	a.port = port
	a.username = username
	a.password = password
	a.mutex = &sync.RWMutex{}
	a.emitEvent = eventEmitter

	conn, err := a.openConnection()
	if err != nil {
		return nil, err
	}

	a.actionsChan = make(chan map[string]string)
	a.responseChans = make(map[string]chan map[string]string)
	a.EventsChan = make(chan map[string]string)

	go func() {
		for {
			a.readErrChan = make(chan error)
			a.writeErrChan = make(chan error)
			chanStopActionWriter := make(chan struct{})

			for {
				time.Sleep(time.Second * 1)

				conn, err = a.openConnection()
				if err != nil {
					go a.emitEvent("error", "AMI Reconnect failed")
				} else {
					a.conn = conn
					go a.streamReader()
					go a.actionWriter(chanStopActionWriter)

					err = a.login()
					if err != nil {
						go a.emitEvent("error", fmt.Sprintf("Asterisk login error: %s", err.Error()))
						continue
					}
					break
				}
			}

			select {
			case err = <-a.readErrChan:
			case err = <-a.writeErrChan:
			}

			chanStopActionWriter <- struct{}{}
			a.mutex.Lock()
			a.connected = false
			a.mutex.Unlock()

			go a.emitEvent("error", fmt.Sprintf("AMI TCP ERROR: %s", err.Error()))
			conn.Close()
		}
	}()

	return a, nil
}

func (a *amiAdapter) actionWriter(stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case action := <-a.actionsChan:
			var data = serialize(action)
			_, err := a.conn.Write(data)
			if err != nil {
				a.writeErrChan <- err
				return
			}
		}
	}
}

func (a *amiAdapter) distribute(event map[string]string) {
	if _, ok := event["Event"]; ok {
		a.EventsChan <- event
		return
	}

	if actionID := event["ActionID"]; actionID != "" {
		a.mutex.RLock()
		resChan := a.responseChans[actionID]
		a.mutex.RUnlock()
		if resChan != nil {
			a.mutex.Lock()
			resChan = a.responseChans[actionID]
			if resChan == nil {
				a.mutex.Unlock()
				return
			}
			delete(a.responseChans, actionID)
			a.mutex.Unlock()
			resChan <- event
		}
	}
}

func (a *amiAdapter) exec(action map[string]string) map[string]string {
	actionID := action["ActionID"]
	if actionID == "" {
		actionID = uuid.NewV4()
		action["ActionID"] = actionID
	}

	// TODO: parse multi-message response
	resChan := make(chan map[string]string)
	a.mutex.Lock()
	a.responseChans[actionID] = resChan
	a.mutex.Unlock()

	a.actionsChan <- action

	time.AfterFunc(actionTimeout, func() {
		a.mutex.RLock()
		_, ok := a.responseChans[actionID]
		a.mutex.RUnlock()
		if ok {
			a.mutex.Lock()
			if a.responseChans[actionID] != nil {
				ch := a.responseChans[actionID]
				delete(a.responseChans, actionID)
				a.mutex.Unlock()
				ch <- map[string]string{"Error": "Timeout"}
				return
			}
			a.mutex.Unlock()
		}
	})

	response := <-resChan

	return response
}

func (a *amiAdapter) login() error {
	var action = map[string]string{
		"Action":   "Login",
		"Username": a.username,
		"Secret":   a.password,
	}

	var result = a.exec(action)
	if result["Response"] != "Success" {
		return errors.New("Login failed: " + result["Message"])
	}

	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.connected = true

	return nil
}

func (a *amiAdapter) online() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.connected
}

func (a *amiAdapter) openConnection() (*net.TCPConn, error) {
	socket := a.ip + ":" + a.port

	raddr, err := net.ResolveTCPAddr("tcp", socket)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTCP("tcp", nil, raddr)
	if err != nil {
		return nil, err
	}

	return conn, err
}

func serialize(data map[string]string) []byte {
	var outBuf bytes.Buffer

	for key := range data {
		value := data[key]

		outBuf.WriteString(key)
		outBuf.WriteString(": ")
		outBuf.WriteString(value)
		outBuf.WriteString("\n")
	}
	outBuf.WriteString("\n")
	return outBuf.Bytes()
}

func readMessage(r *bufio.Reader) (m map[string]string, err error) {
	m = make(map[string]string)
	var responseFollows bool
	for {
		kv, _, err := r.ReadLine()
		if len(kv) == 0 {
			return m, err
		}

		var key string
		i := bytes.IndexByte(kv, ':')
		if i >= 0 {
			endKey := i
			for endKey > 0 && kv[endKey-1] == ' ' {
				endKey--
			}
			key = string(kv[:endKey])
		}

		if key == "" && !responseFollows {
			if err != nil {
				return m, err
			}

			continue
		}

		if responseFollows && key != "Privilege" {
			if string(kv) != "--END COMMAND--" {
				m["CommandResponse"] = fmt.Sprintf("%s\n%s", m["CommandResponse"], string(kv))
			}
			if err != nil {
				return m, err
			}

			continue
		}

		i++
		for i < len(kv) && (kv[i] == ' ' || kv[i] == '\t') {
			i++
		}
		value := string(kv[i:])

		if key == "Response" && value == "Follows" {
			responseFollows = true
		}

		m[key] = value

		if err != nil {
			return m, err
		}
	}
}

func (a *amiAdapter) streamReader() {
	greetings := make([]byte, 100)
	n, err := a.conn.Read(greetings)
	if err != nil {
		a.readErrChan <- err
		return
	}
	if n > 2 {
		greetings = greetings[:n-2]
	}
	go a.emitEvent("connect", fmt.Sprintf("Connected to %s %s:%s", greetings, a.ip, a.port))

	bufReader := bufio.NewReader(a.conn)
	var event map[string]string
	for {
		event, err = readMessage(bufReader)
		if err != nil {
			a.readErrChan <- err
			return
		}
		a.distribute(event)
	}
}
