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

	a.actionsChan = make(chan map[string]string)
	a.responseChans = make(map[string]chan map[string]string)
	a.EventsChan = make(chan map[string]string, 1000)

	go func() {
		for {
			var err error

			a.readErrChan = make(chan error)
			a.writeErrChan = make(chan error)
			chanStop := make(chan struct{})

			for {
				time.Sleep(time.Second * 1)

				err = a.openConnection()
				if err == nil {
					go a.streamReader(chanStop)
					go a.actionWriter(chanStop)

					greetings := make([]byte, 100)
					n, err := a.conn.Read(greetings)
					if err != nil {
						go a.emitEvent("error", fmt.Sprintf("Asterisk connection error: %s", err.Error()))
						continue
					}

					err = a.login()
					if err != nil {
						go a.emitEvent("error", fmt.Sprintf("Asterisk login error: %s", err.Error()))
						continue
					}

					if n > 2 {
						greetings = greetings[:n-2]
					}
					go a.emitEvent("connect", string(greetings))

					break
				}

				a.emitEvent("error", "AMI Reconnect failed")
			}

			a.mutex.Lock()
			a.connected = true
			a.mutex.Unlock()

			select {
			case err = <-a.readErrChan:
			case err = <-a.writeErrChan:
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

func (a *amiAdapter) actionWriter(stop chan struct{}) {
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
				a.writeErrChan <- err
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

func (a *amiAdapter) openConnection() error {
	socket := a.ip + ":" + a.port

	raddr, err := net.ResolveTCPAddr("tcp", socket)
	if err != nil {
		return err
	}

	a.conn, err = net.DialTCP("tcp", nil, raddr)
	if err != nil {
		return err
	}

	return nil
}

func readMessage(r *bufio.Reader) (m map[string]string, err error) {
	m = make(map[string]string)
	var responseFollows bool
	for {
		kv, _, err := r.ReadLine()
		if len(kv) == 0 {
			return m, err
		}
		kv = toUnicode(kv)

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

func toUnicode(bs []byte) []byte {
	var buffer bytes.Buffer
	for _, b := range bs {
		buffer.WriteRune(rune(b))
	}
	return buffer.Bytes()
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

func (a *amiAdapter) streamReader(stop chan struct{}) {
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
			a.readErrChan <- err
		case event := <-chanEvents:
			a.distribute(event)
		}
	}
}
