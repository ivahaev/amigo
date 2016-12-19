package amigo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type amiAdapter struct {
	ip       string
	port     string
	username string
	password string

	conn          *net.TCPConn
	connected     bool
	chanActions   chan map[string]string
	chanResponses chan map[string]string
	chanEvents    chan map[string]string
	chanErr       chan error
	mutex         *sync.RWMutex
	emitEvent     func(string, string)
}

func newAMIAdapter(ip string, port string, eventEmitter func(string, string)) (*amiAdapter, error) {
	var a = new(amiAdapter)
	a.ip = ip
	a.port = port
	a.mutex = &sync.RWMutex{}
	a.emitEvent = eventEmitter

	conn, err := a.openConnection()
	if err != nil {
		return nil, err
	}

	a.conn = conn
	a.chanActions = make(chan map[string]string)

	chanOutStreamReader, chanErrStreamReader := a.streamReader()
	a.chanErr = chanErrStreamReader
	chanQuitActionWriter := a.actionWriter()
	a.chanResponses, a.chanEvents = classifier(chanOutStreamReader)

	go func() {
		for {
			err := <-chanErrStreamReader
			chanQuitActionWriter <- true
			a.mutex.Lock()
			a.connected = false
			a.mutex.Unlock()

			go a.emitEvent("error", fmt.Sprintf("AMI TCP ERROR: %s", err.Error()))
			conn.Close()

			for {
				time.Sleep(time.Second * 1)

				conn, err = a.openConnection()
				if err != nil {
					go a.emitEvent("error", "AMI Reconnect failed")
				} else {
					a.conn = conn
					chanOutStreamReader, chanErrStreamReader = a.streamReader()
					a.chanErr = chanErrStreamReader
					chanQuitActionWriter = a.actionWriter()

					_, err = a.Login(a.username, a.password)
					if err != nil {
						go a.emitEvent("error", fmt.Sprintf("Asterisk login error: %s", err.Error()))
					}
					break
				}
			}
		}
	}()

	return a, nil
}

func (a *amiAdapter) Connected() bool {
	return a.connected
}

func (a *amiAdapter) Login(username string, password string) (chan map[string]string, error) {
	a.username = username
	a.password = password

	var action = map[string]string{
		"Action":   "Login",
		"Username": a.username,
		"Secret":   a.password,
	}

	var result = a.Exec(action)

	if result["Response"] != "Success" {
		return nil, errors.New("Login failed: " + result["Message"])
	}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.connected = true

	return a.chanEvents, nil
}

func (a *amiAdapter) Exec(action map[string]string) map[string]string {
	a.chanActions <- action
	var response = <-a.chanResponses
	return response
}

func (a *amiAdapter) streamReader() (chanOut chan map[string]string, chanErr chan error) {
	chanOut = make(chan map[string]string)
	chanErr = make(chan error)

	bufReader := bufio.NewReader(a.conn)
	greetings := make([]byte, 100)
	n, err := bufReader.Read(greetings)
	if err != nil {
		chanErr <- err
		return
	}
	if n > 2 {
		greetings = greetings[:n-2]
	}
	go a.emitEvent("connect", fmt.Sprintf("Connected to %s %s:%s", greetings, a.ip, a.port))

	go func() {
		var b map[string]string
		for {
			b, err = readMessage(bufReader)
			if err != nil {
				chanErr <- err
				return
			}
			chanOut <- b
		}
	}()

	return chanOut, chanErr
}

func (a *amiAdapter) actionWriter() (chanQuit chan bool) {
	chanQuit = make(chan bool)

	go func() {
		for {
			select {
			case action := <-a.chanActions:
				{
					var data = serialize(action)
					_, err := a.conn.Write(data)
					if err != nil {
						a.chanErr <- err
					}
				}
			case <-chanQuit:
				{
					return
				}
			}
		}
	}()

	return chanQuit
}

func classifier(in chan map[string]string) (chanOutResponses chan map[string]string, chanOutEvents chan map[string]string) {
	chanOutResponses = make(chan map[string]string)
	chanOutEvents = make(chan map[string]string)

	go func() {
		for {
			data := <-in

			if _, ok := data["Event"]; ok {
				chanOutEvents <- data
				continue
			}

			if _, ok := data["Response"]; ok {
				chanOutResponses <- data
			}
		}
	}()

	return chanOutResponses, chanOutEvents
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
	for {
		kv, _, err := r.ReadLine()
		if len(kv) == 0 {
			if err == io.EOF && len(m) == 0 {
				err = nil
			}
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
		} else {
			key = "Extra"
		}

		if key == "" {
			continue
		}

		// Skip initial spaces in value.
		i++ // skip colon
		for i < len(kv) && (kv[i] == ' ' || kv[i] == '\t') {
			i++
		}
		value := string(kv[i:])

		if m[key] == "" {
			m[key] = value
		} else {
			if value == "--END COMMAND--" {
				continue
			}
			m[key] = m[key] + " " + value
		}

		if err != nil {
			return m, err
		}
	}
}
