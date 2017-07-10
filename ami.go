package amigo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/ivahaev/amigo/uuid"
)

var (
	actionTimeout = 3 * time.Second
	dialTimeout   = 10 * time.Second
)

type amiAdapter struct {
	EventsChan chan map[string]string

	dialString string
	username   string
	password   string

	conn          net.Conn
	connected     bool
	actionTimeout time.Duration
	dialTimeout   time.Duration

	actionsChan   chan map[string]string
	responseChans map[string]chan map[string]string
	mutex         *sync.RWMutex
	emitEvent     func(string, string)
}

func newAMIAdapter(s *Settings, eventEmitter func(string, string)) (*amiAdapter, error) {
	var a = new(amiAdapter)
	a.dialString = fmt.Sprintf("%s:%s", s.Host, s.Port)
	a.username = s.Username
	a.password = s.Password
	a.actionTimeout = s.ActionTimeout
	a.dialTimeout = s.DialTimeout
	a.mutex = &sync.RWMutex{}
	a.emitEvent = eventEmitter

	a.actionsChan = make(chan map[string]string)
	a.responseChans = make(map[string]chan map[string]string)
	a.EventsChan = make(chan map[string]string, 1000)

	go func() {
		for {
			var err error
			readErrChan := make(chan error)
			writeErrChan := make(chan error)
			chanStop := make(chan struct{})
			for {
				conn, err := a.openConnection()
				if err == nil {
					a.conn = conn
					go a.streamReader(chanStop, readErrChan)
					go a.actionWriter(chanStop, writeErrChan)

					greetings := make([]byte, 100)
					n, err := a.conn.Read(greetings)
					if err != nil {
						go a.emitEvent("error", fmt.Sprintf("Asterisk connection error: %s", err.Error()))
						a.conn.Close()
						continue
					}

					err = a.login()
					if err != nil {
						go a.emitEvent("error", fmt.Sprintf("Asterisk login error: %s", err.Error()))
						a.conn.Close()
						continue
					}

					if n > 2 {
						greetings = greetings[:n-2]
					}
					go a.emitEvent("connect", string(greetings))

					break
				}

				a.emitEvent("error", "AMI Reconnect failed")
				time.Sleep(time.Second * 1)
			}

			a.mutex.Lock()
			a.connected = true
			a.mutex.Unlock()

			select {
			case err = <-readErrChan:
			case err = <-writeErrChan:
			}

			close(chanStop)
			a.mutex.Lock()
			a.connected = false
			a.mutex.Unlock()

			go a.emitEvent("error", fmt.Sprintf("AMI TCP ERROR: %s", err.Error()))
			a.conn.Close()
		}
	}()

	return a, nil
}

func (a *amiAdapter) actionWriter(stop chan struct{}, writeErrChan chan error) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		select {
		case <-stop:
			return
		case action := <-a.actionsChan:
			var data = serialize(action)
			_, err := a.conn.Write(data)
			if err != nil {
				writeErrChan <- err
				return
			}
		}
	}
}

func (a *amiAdapter) distribute(event map[string]string) {
	actionID := event["ActionID"]
	a.EventsChan <- event

	if actionID != "" {
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

	time.AfterFunc(a.actionTimeout, func() {
		a.mutex.RLock()
		_, ok := a.responseChans[actionID]
		a.mutex.RUnlock()
		if ok {
			a.mutex.Lock()
			if ch, ok := a.responseChans[actionID]; ok {
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
	if result["Response"] != "Success" && result["Message"] != "Authentication accepted" {
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

func (a *amiAdapter) openConnection() (net.Conn, error) {
	return net.DialTimeout("tcp", a.dialString, a.dialTimeout)
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
				if len(m["CommandResponse"]) == 0 {
					m["CommandResponse"] = string(kv)
				} else {
					m["CommandResponse"] = fmt.Sprintf("%s\n%s", m["CommandResponse"], string(kv))
				}
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

func serialize(data map[string]string) []byte {
	var outBuf bytes.Buffer

	for key := range data {
		outBuf.WriteString(key)
		outBuf.WriteString(": ")
		outBuf.WriteString(data[key])
		outBuf.WriteString("\n")
	}
	outBuf.WriteString("\n")

	return outBuf.Bytes()
}

func (a *amiAdapter) streamReader(stop chan struct{}, readErrChan chan error) {
	chanErr := make(chan error)
	chanEvents := make(chan map[string]string)

	go func() {
		bufReader := bufio.NewReader(a.conn)
		for i := 0; ; i++ {
			var event map[string]string
			var err error
			event, err = readMessage(bufReader)
			if err != nil {
				chanErr <- err
				return
			}

			event["#"] = strconv.Itoa(i)
			event["Time"] = time.Now().Format(time.RFC3339Nano)
			chanEvents <- event
		}
	}()

	for {
		select {
		case <-stop:
			return
		default:
		}

		select {
		case <-stop:
			return
		case err := <-chanErr:
			readErrChan <- err
		case event := <-chanEvents:
			a.distribute(event)
		}
	}
}
